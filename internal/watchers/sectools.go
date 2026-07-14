package watchers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// SecToolsWatcher tails ClamAV and rkhunter log files for security findings.
// It polls at a configurable interval, tracking byte offsets so only new lines
// are processed each tick. Missing log files are silently skipped (tool not installed).
type SecToolsWatcher struct {
	Base
	interval time.Duration
	offsets  map[string]int64 // log path → last read byte offset
}

func NewSecToolsWatcher(cfg *config.Config, bus *eventbus.Bus) *SecToolsWatcher {
	interval, err := time.ParseDuration(cfg.SecurityTools.PollInterval)
	if err != nil {
		interval = 30 * time.Second
	}
	return &SecToolsWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		interval: interval,
		offsets:  make(map[string]int64),
	}
}

func (w *SecToolsWatcher) Name() string { return "sectools" }

func (w *SecToolsWatcher) Start(ctx context.Context) error {
	slog.Info("starting sectools watcher",
		"interval", w.interval,
		"clamav_log", w.Cfg.SecurityTools.ClamAVLog,
		"rkhunter_log", w.Cfg.SecurityTools.RKHunterLog,
	)

	// Seek to end of each log file on startup so historical entries are ignored.
	for _, path := range []string{w.Cfg.SecurityTools.ClamAVLog, w.Cfg.SecurityTools.RKHunterLog} {
		if info, err := os.Stat(path); err == nil {
			w.offsets[path] = info.Size()
		} else {
			slog.Debug("sectools: log not found at startup (tool may not be installed)", "path", path)
		}
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.scanLog(w.Cfg.SecurityTools.ClamAVLog, models.EventMalwareFound, isClamAVMatch)
			w.scanLog(w.Cfg.SecurityTools.RKHunterLog, models.EventRootkitWarning, isRKHunterMatch)
		}
	}
}

func (w *SecToolsWatcher) Stop() error { return nil }

// scanLog reads new lines appended to path since the last check.
func (w *SecToolsWatcher) scanLog(path string, evType models.EventType, match func(string) bool) {
	info, err := os.Stat(path)
	if err != nil {
		// Tool not installed or log not yet created — silent no-op.
		return
	}

	// Detect log rotation: if the file shrank, start over from the beginning.
	if info.Size() < w.offsets[path] {
		slog.Debug("sectools: log rotation detected, resetting offset", "path", path)
		w.offsets[path] = 0
	}

	f, err := os.Open(path)
	if err != nil {
		slog.Warn("sectools: could not open log", "path", path, "error", err)
		return
	}
	defer f.Close()

	if _, err := f.Seek(w.offsets[path], io.SeekStart); err != nil {
		slog.Warn("sectools: seek failed", "path", path, "error", err)
		return
	}

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if evType == models.EventRootkitWarning {
		for _, finding := range parseRKHunterFindings(lines) {
			w.publishFinding(path, finding)
		}
	} else {
		for _, line := range lines {
			if strings.Contains(line, "PIGUARD_SCAN_ERROR:") {
				w.publishFinding(path, securityFinding{
					eventType: models.EventSecurityScanFailed,
					severity:  models.SeverityWarning,
					message:   strings.TrimSpace(line),
				})
				continue
			}
			if match(line) {
				w.publishFinding(path, securityFinding{
					eventType: evType,
					severity:  models.SeverityCritical,
					message:   line,
				})
			}
		}
	}

	// Store the new offset (position after last complete line).
	pos, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		w.offsets[path] = pos
	}
}

type securityFinding struct {
	eventType models.EventType
	severity  models.Severity
	message   string
	details   string
}

func (w *SecToolsWatcher) publishFinding(path string, finding securityFinding) {
	hostname, _ := os.Hostname()
	details := fmt.Sprintf("Log file: %s", path)
	if finding.details != "" {
		details += "\n" + finding.details
	}
	slog.Info("sectools: match found", "type", finding.eventType, "message", finding.message)
	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("%s-%d", string(finding.eventType), time.Now().UnixNano()),
		Type:      finding.eventType,
		Severity:  finding.severity,
		Hostname:  hostname,
		Timestamp: time.Now(),
		Message:   finding.message,
		Details:   details,
		Suggested: suggestedAction(finding.eventType),
		Source:    "sectools",
	})
}

func parseRKHunterFindings(lines []string) []securityFinding {
	var findings []securityFinding
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, "PIGUARD_SCAN_ERROR:") {
			findings = append(findings, securityFinding{
				eventType: models.EventSecurityScanFailed,
				severity:  models.SeverityWarning,
				message:   strings.TrimSpace(line),
			})
			continue
		}
		if !isRKHunterMatch(line) {
			continue
		}

		finding := securityFinding{
			eventType: models.EventRootkitWarning,
			severity:  models.SeverityCritical,
			message:   line,
		}
		if strings.Contains(line, "file properties have changed") {
			finding.severity = models.SeverityWarning
			var context []string
			for j := i + 1; j < len(lines) && j <= i+8; j++ {
				next := strings.TrimSpace(stripRKHunterTimestamp(lines[j]))
				if next == "" {
					continue
				}
				if strings.HasPrefix(next, "File:") {
					file := strings.TrimSpace(strings.TrimPrefix(next, "File:"))
					finding.message = "File properties changed: " + file
				}
				if strings.HasPrefix(next, "File:") || strings.HasPrefix(next, "Current hash:") ||
					strings.HasPrefix(next, "Stored hash") || strings.HasPrefix(next, "Current inode:") ||
					strings.HasPrefix(next, "Current file modification time:") || strings.HasPrefix(next, "Stored file modification time") {
					context = append(context, next)
				}
			}
			finding.details = strings.Join(context, "\n")
		}
		findings = append(findings, finding)
	}
	return findings
}

func stripRKHunterTimestamp(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "[") {
		if end := strings.Index(line, "]"); end >= 0 {
			return strings.TrimSpace(line[end+1:])
		}
	}
	return line
}

// isClamAVMatch returns true for genuine ClamAV FOUND lines.
// Filters out harmless stat-error lines that also contain "No such file".
func isClamAVMatch(line string) bool {
	return strings.Contains(line, "FOUND") && !strings.Contains(line, "No such file")
}

// isRKHunterMatch returns true for rkhunter Warning: lines.
func isRKHunterMatch(line string) bool {
	return strings.Contains(line, "Warning:")
}

func suggestedAction(evType models.EventType) string {
	switch evType {
	case models.EventMalwareFound:
		return "Quarantine or remove the flagged file. Run: sudo clamscan -r --remove /path/to/file"
	case models.EventRootkitWarning:
		return "Review the full report and verify the affected package before updating the baseline: sudo rkhunter --check --skip-keypress --report-warnings-only"
	case models.EventSecurityScanFailed:
		return "Inspect the scan service: sudo journalctl -u piguard-security-scan.service"
	}
	return ""
}
