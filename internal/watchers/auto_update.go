package watchers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// AutoUpdateWatcher runs scheduled apt upgrades and publishes results as events.
type AutoUpdateWatcher struct {
	Base
	mu         sync.RWMutex
	dayOfWeek  time.Weekday // -1 means daily
	daily      bool
	timeHHMM   string
	runApt     func(args ...string) (string, error)
	fileExist  func(path string) bool
	runReboot  func() error
	sleepFunc  func(d time.Duration)
}

func NewAutoUpdateWatcher(cfg *config.Config, bus *eventbus.Bus) *AutoUpdateWatcher {
	w := &AutoUpdateWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		timeHHMM: cfg.AutoUpdate.Time,
	}

	day := strings.ToLower(cfg.AutoUpdate.DayOfWeek)
	if day == "daily" || day == "" {
		w.daily = true
	} else {
		w.dayOfWeek = parseWeekday(day)
	}

	if w.timeHHMM == "" {
		w.timeHHMM = "03:00"
	}

	w.runApt = func(args ...string) (string, error) {
		out, err := exec.Command("apt-get", args...).CombinedOutput()
		return string(out), err
	}
	w.fileExist = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	w.runReboot = func() error {
		return exec.Command("reboot").Run()
	}
	w.sleepFunc = time.Sleep

	return w
}

func (w *AutoUpdateWatcher) Name() string { return "auto-update" }
func (w *AutoUpdateWatcher) Stop() error  { return nil }

func (w *AutoUpdateWatcher) Start(ctx context.Context) error {
	slog.Info("starting auto-update watcher", "day", w.Cfg.AutoUpdate.DayOfWeek, "time", w.timeHHMM,
		"enabled", w.Cfg.AutoUpdate.Enabled)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.mu.RLock()
			enabled := w.Cfg.AutoUpdate.Enabled
			match := w.isScheduleMatchLocked(time.Now())
			w.mu.RUnlock()
			if enabled && match {
				w.runUpgrade()
			}
		}
	}
}

// SetDay updates the schedule day at runtime. Accepts "daily", "sunday", "monday", etc.
func (w *AutoUpdateWatcher) SetDay(day string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := strings.ToLower(day)
	if d == "daily" || d == "" {
		w.daily = true
	} else {
		w.daily = false
		w.dayOfWeek = parseWeekday(d)
	}
	w.Cfg.AutoUpdate.DayOfWeek = day
}

// SetTime updates the schedule time at runtime. Format: "HH:MM" (24h).
func (w *AutoUpdateWatcher) SetTime(t string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timeHHMM = t
	w.Cfg.AutoUpdate.Time = t
}

// SetAutoReboot toggles automatic reboot after updates at runtime.
func (w *AutoUpdateWatcher) SetAutoReboot(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Cfg.AutoUpdate.AutoReboot = enabled
}

// SetEnabled toggles the auto-update watcher on/off at runtime.
func (w *AutoUpdateWatcher) SetEnabled(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Cfg.AutoUpdate.Enabled = enabled
}

// GetStatus returns a formatted string describing the current auto-update configuration.
func (w *AutoUpdateWatcher) GetStatus() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.Cfg.AutoUpdate.Enabled {
		return "⏰ <b>Auto-Update</b>\n\n❌ Disabled"
	}

	day := w.Cfg.AutoUpdate.DayOfWeek
	if day == "" {
		day = "daily"
	}

	reboot := "off"
	if w.Cfg.AutoUpdate.AutoReboot {
		reboot = fmt.Sprintf("on (%d min delay)", w.Cfg.AutoUpdate.RebootDelayMinutes)
	}

	return fmt.Sprintf("⏰ <b>Auto-Update</b>\n\n"+
		"  ✅ Enabled\n"+
		"  📅 Schedule: %s at %s\n"+
		"  🔄 Auto-reboot: %s",
		capitalise(day), w.timeHHMM, reboot)
}

// isScheduleMatch checks if now matches the configured schedule (thread-safe).
func (w *AutoUpdateWatcher) isScheduleMatch(now time.Time) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isScheduleMatchLocked(now)
}

// isScheduleMatchLocked checks schedule match. Caller must hold mu.RLock().
func (w *AutoUpdateWatcher) isScheduleMatchLocked(now time.Time) bool {
	hhmm := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	if hhmm != w.timeHHMM {
		return false
	}
	if w.daily {
		return true
	}
	return now.Weekday() == w.dayOfWeek
}

func (w *AutoUpdateWatcher) runUpgrade() {
	hostname, _ := os.Hostname()
	slog.Info("auto-update: starting apt upgrade")

	// apt-get update
	updateOut, err := w.runApt("update")
	if err != nil {
		slog.Error("auto-update: apt-get update failed", "error", err)
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("system.update_failed-%d", time.Now().UnixNano()),
			Type:      models.EventSystemUpdateFailed,
			Severity:  models.SeverityWarning,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   "System update failed: apt-get update error",
			Details:   truncateStr(updateOut, 500),
			Suggested: "Run manually: sudo apt-get update",
			Source:    "auto-update",
		})
		return
	}

	// apt-get upgrade -y
	upgradeOut, err := w.runApt("upgrade", "-y")
	if err != nil {
		slog.Error("auto-update: apt-get upgrade failed", "error", err)
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("system.update_failed-%d", time.Now().UnixNano()),
			Type:      models.EventSystemUpdateFailed,
			Severity:  models.SeverityWarning,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   "System update failed: apt-get upgrade error",
			Details:   truncateStr(upgradeOut, 500),
			Suggested: "Run manually: sudo apt-get upgrade -y",
			Source:    "auto-update",
		})
		return
	}

	count := parseUpgradeCount(upgradeOut)
	msg := fmt.Sprintf("System updated: %d package(s) upgraded", count)
	if count == 0 {
		msg = "System update: already up to date"
	}

	// Check if reboot is required
	rebootNeeded := w.fileExist("/var/run/reboot-required")
	if rebootNeeded {
		msg += " (reboot required)"
	}

	slog.Info("auto-update: complete", "upgraded", count, "reboot_needed", rebootNeeded)

	severity := models.SeverityInfo
	suggested := ""
	autoRebooting := rebootNeeded && w.Cfg.AutoUpdate.AutoReboot
	if rebootNeeded && !autoRebooting {
		severity = models.SeverityWarning
		suggested = "Reboot needed — send /reboot CONFIRM via Telegram"
	}
	if autoRebooting {
		delay := w.Cfg.AutoUpdate.RebootDelayMinutes
		if delay <= 0 {
			delay = 5
		}
		severity = models.SeverityWarning
		msg += fmt.Sprintf(" — rebooting in %d min", delay)
		suggested = fmt.Sprintf("Auto-reboot in %d minutes", delay)
	}

	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("system.updated-%d", time.Now().UnixNano()),
		Type:      models.EventSystemUpdated,
		Severity:  severity,
		Hostname:  hostname,
		Timestamp: time.Now(),
		Message:   msg,
		Details:   truncateStr(upgradeOut, 500),
		Suggested: suggested,
		Source:    "auto-update",
	})

	if autoRebooting {
		delay := w.Cfg.AutoUpdate.RebootDelayMinutes
		if delay <= 0 {
			delay = 5
		}
		slog.Info("auto-update: rebooting", "delay_minutes", delay)
		w.sleepFunc(time.Duration(delay) * time.Minute)
		if err := w.runReboot(); err != nil {
			slog.Error("auto-update: reboot failed", "error", err)
		}
	}
}

// CheckAvailable runs `apt list --upgradable` and returns formatted output.
func (w *AutoUpdateWatcher) CheckAvailable() (string, error) {
	out, err := w.runApt("list", "--upgradable")
	if err != nil {
		return "", fmt.Errorf("apt list --upgradable: %w", err)
	}
	return out, nil
}

var upgradeCountRe = regexp.MustCompile(`(\d+)\s+upgraded`)

// parseUpgradeCount extracts the number of upgraded packages from apt output.
// It looks for "N upgraded" in the summary line.
func parseUpgradeCount(output string) int {
	matches := upgradeCountRe.FindStringSubmatch(output)
	if len(matches) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(matches[1])
	return n
}

func parseWeekday(s string) time.Weekday {
	switch strings.ToLower(s) {
	case "sunday", "sun":
		return time.Sunday
	case "monday", "mon":
		return time.Monday
	case "tuesday", "tue":
		return time.Tuesday
	case "wednesday", "wed":
		return time.Wednesday
	case "thursday", "thu":
		return time.Thursday
	case "friday", "fri":
		return time.Friday
	case "saturday", "sat":
		return time.Saturday
	default:
		return time.Sunday
	}
}

// capitalise returns s with the first letter uppercased.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// truncateStr limits a string to max bytes (used for event details).
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
