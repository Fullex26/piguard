package watchers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// AuthLogWatcher monitors /var/log/auth.log for SSH brute-force attempts,
// sudo failures, and (optionally) successful SSH logins.
type AuthLogWatcher struct {
	Base
	interval       time.Duration
	offset         int64
	logPath        string
	threshold      int
	window         time.Duration
	alertOnLogin   bool
	failedAttempts map[string][]time.Time // IP → timestamps of failed logins
	alertedIPs     map[string]time.Time   // IP → last alert time (prevent re-alerting same burst)
}

func NewAuthLogWatcher(cfg *config.Config, bus *eventbus.Bus) *AuthLogWatcher {
	interval, err := time.ParseDuration(cfg.AuthLog.PollInterval)
	if err != nil {
		interval = 10 * time.Second
	}
	window, err := time.ParseDuration(cfg.AuthLog.BruteForceWindow)
	if err != nil {
		window = 5 * time.Minute
	}
	threshold := cfg.AuthLog.BruteForceThreshold
	if threshold <= 0 {
		threshold = 5
	}
	return &AuthLogWatcher{
		Base:           Base{Cfg: cfg, Bus: bus},
		interval:       interval,
		logPath:        cfg.AuthLog.LogPath,
		threshold:      threshold,
		window:         window,
		alertOnLogin:   cfg.AuthLog.AlertOnLogin,
		failedAttempts: make(map[string][]time.Time),
		alertedIPs:     make(map[string]time.Time),
	}
}

func (w *AuthLogWatcher) Name() string { return "auth-log" }
func (w *AuthLogWatcher) Stop() error  { return nil }

func (w *AuthLogWatcher) Start(ctx context.Context) error {
	slog.Info("starting auth-log watcher",
		"interval", w.interval,
		"log_path", w.logPath,
		"threshold", w.threshold,
		"window", w.window,
	)

	// Seek to end on startup so historical entries are ignored
	if info, err := os.Stat(w.logPath); err == nil {
		w.offset = info.Size()
	} else {
		slog.Debug("auth-log: log not found at startup", "path", w.logPath)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.scanLog()
			w.cleanupAttempts()
		}
	}
}

func (w *AuthLogWatcher) scanLog() {
	info, err := os.Stat(w.logPath)
	if err != nil {
		return // log file doesn't exist — silently skip
	}

	// Detect log rotation: file shrank
	if info.Size() < w.offset {
		slog.Debug("auth-log: rotation detected, resetting offset", "path", w.logPath)
		w.offset = 0
	}

	f, err := os.Open(w.logPath)
	if err != nil {
		slog.Warn("auth-log: could not open log", "path", w.logPath, "error", err)
		return
	}
	defer f.Close()

	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		slog.Warn("auth-log: seek failed", "path", w.logPath, "error", err)
		return
	}

	hostname, _ := os.Hostname()
	scanner := bufio.NewScanner(f)
	now := time.Now()

	for scanner.Scan() {
		line := scanner.Text()
		w.processLine(line, hostname, now)
	}

	pos, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		w.offset = pos
	}
}

func (w *AuthLogWatcher) processLine(line, hostname string, now time.Time) {
	// SSH failed login
	if ip, user, ok := ParseSSHFailed(line); ok {
		w.failedAttempts[ip] = append(w.failedAttempts[ip], now)

		// Check brute force threshold
		recent := w.recentAttempts(ip, now)
		if recent >= w.threshold {
			// Only alert once per burst
			if lastAlert, alerted := w.alertedIPs[ip]; !alerted || now.Sub(lastAlert) > w.window {
				w.alertedIPs[ip] = now
				w.Bus.Publish(models.Event{
					ID:        fmt.Sprintf("ssh.bruteforce-%s-%d", ip, now.UnixNano()),
					Type:      models.EventSSHBruteForce,
					Severity:  models.SeverityCritical,
					Hostname:  hostname,
					Timestamp: now,
					Message:   fmt.Sprintf("SSH brute force: %d failed attempts from %s", recent, ip),
					Details:   fmt.Sprintf("Last failed user: %s", user),
					Suggested: fmt.Sprintf("Block IP: sudo iptables -A INPUT -s %s -j DROP", ip),
					Source:    "auth-log",
				})
			}
		}
		return
	}

	// Sudo failure
	if IsSudoFailure(line) {
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("sudo.failure-%d", now.UnixNano()),
			Type:      models.EventSudoFailure,
			Severity:  models.SeverityWarning,
			Hostname:  hostname,
			Timestamp: now,
			Message:   "Sudo authentication failure detected",
			Details:   line,
			Suggested: "Check /var/log/auth.log for unauthorized sudo attempts",
			Source:    "auth-log",
		})
		return
	}

	// SSH login (optional)
	if w.alertOnLogin {
		if ip, user, ok := ParseSSHLogin(line); ok {
			w.Bus.Publish(models.Event{
				ID:        fmt.Sprintf("ssh.login-%d", now.UnixNano()),
				Type:      models.EventSSHLogin,
				Severity:  models.SeverityInfo,
				Hostname:  hostname,
				Timestamp: now,
				Message:   fmt.Sprintf("SSH login: %s from %s", user, ip),
				Details:   line,
				Source:    "auth-log",
			})
		}
	}
}

// recentAttempts counts failed attempts from ip within the brute force window.
func (w *AuthLogWatcher) recentAttempts(ip string, now time.Time) int {
	cutoff := now.Add(-w.window)
	count := 0
	for _, t := range w.failedAttempts[ip] {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

// cleanupAttempts prunes entries outside the window.
func (w *AuthLogWatcher) cleanupAttempts() {
	now := time.Now()
	cutoff := now.Add(-w.window)
	for ip, times := range w.failedAttempts {
		var recent []time.Time
		for _, t := range times {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(w.failedAttempts, ip)
		} else {
			w.failedAttempts[ip] = recent
		}
	}

	// Also clean up alerted IPs
	for ip, t := range w.alertedIPs {
		if now.Sub(t) > w.window*2 {
			delete(w.alertedIPs, ip)
		}
	}
}

// ── Line parsers (exported for testing) ──

var (
	// Matches: "Failed password for <user> from <ip> port <N> ssh2"
	// and:     "Failed password for invalid user <user> from <ip> port <N> ssh2"
	reSSHFailed = regexp.MustCompile(`Failed password for (?:invalid user )?(\S+) from (\S+) port \d+`)

	// Matches: "Accepted publickey for <user> from <ip> port <N> ssh2"
	// and:     "Accepted password for <user> from <ip> port <N> ssh2"
	reSSHLogin = regexp.MustCompile(`Accepted (?:publickey|password|keyboard-interactive) for (\S+) from (\S+) port \d+`)
)

// ParseSSHFailed extracts IP and username from a failed SSH password line.
func ParseSSHFailed(line string) (ip, user string, ok bool) {
	m := reSSHFailed.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[2], m[1], true
}

// IsSudoFailure returns true if the line indicates a sudo authentication failure.
func IsSudoFailure(line string) bool {
	return strings.Contains(line, "pam_unix(sudo:auth): authentication failure")
}

// ParseSSHLogin extracts IP and username from a successful SSH login line.
func ParseSSHLogin(line string) (ip, user string, ok bool) {
	m := reSSHLogin.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[2], m[1], true
}
