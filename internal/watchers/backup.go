package watchers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/store"
	"github.com/Fullex26/piguard/pkg/models"
)

// BackupWatcher runs scheduled rsync backups and publishes results as events.
type BackupWatcher struct {
	Base
	store     *store.Store
	running   atomic.Bool
	dayOfWeek time.Weekday
	daily     bool
	timeHHMM  string

	// Injectable for testing
	runRsync  func(args ...string) (string, error)
	runCmd    func(name string, args ...string) (string, error)
	fileExist func(path string) bool
	statPath  func(path string) (os.FileInfo, error)
	nowFunc   func() time.Time
}

func NewBackupWatcher(cfg *config.Config, bus *eventbus.Bus, db *store.Store) *BackupWatcher {
	w := &BackupWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		store:    db,
		timeHHMM: cfg.Backup.Time,
	}

	day := strings.ToLower(cfg.Backup.DayOfWeek)
	if day == "daily" || day == "" {
		w.daily = true
	} else {
		w.dayOfWeek = parseWeekday(day)
	}

	if w.timeHHMM == "" {
		w.timeHHMM = "02:00"
	}

	w.runRsync = func(args ...string) (string, error) {
		out, err := exec.Command("rsync", args...).CombinedOutput()
		return string(out), err
	}
	w.runCmd = func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).CombinedOutput()
		return string(out), err
	}
	w.fileExist = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	w.statPath = os.Stat
	w.nowFunc = time.Now

	return w
}

func (w *BackupWatcher) Name() string { return "backup" }
func (w *BackupWatcher) Stop() error  { return nil }

func (w *BackupWatcher) Start(ctx context.Context) error {
	slog.Info("starting backup watcher", "day", w.Cfg.Backup.DayOfWeek, "time", w.timeHHMM)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if w.isScheduleMatch(w.nowFunc()) {
				w.RunBackup()
			}
		}
	}
}

func (w *BackupWatcher) isScheduleMatch(now time.Time) bool {
	hhmm := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	if hhmm != w.timeHHMM {
		return false
	}
	if w.daily {
		return true
	}
	return now.Weekday() == w.dayOfWeek
}

// RunBackup executes a backup and returns a human-readable result string.
// It is exported so the Telegram bot can call it for /backup now.
func (w *BackupWatcher) RunBackup() string {
	if !w.running.CompareAndSwap(false, true) {
		return "⚠️ A backup is already running."
	}
	defer w.running.Store(false)

	hostname, _ := os.Hostname()
	now := w.nowFunc()
	startTime := now

	// Publish started event
	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("backup.started-%d", now.UnixNano()),
		Type:      models.EventBackupStarted,
		Severity:  models.SeverityInfo,
		Hostname:  hostname,
		Timestamp: now,
		Message:   "Backup started",
		Source:    "backup",
	})

	// Pre-flight: check rsync is installed
	if !w.fileExist("/usr/bin/rsync") {
		msg := "Backup failed: rsync not installed"
		w.publishFailure(hostname, msg, "Install rsync: sudo apt install rsync")
		w.persistState("failed", msg, "", "", now)
		return "❌ " + msg
	}

	dest := w.Cfg.Backup.Destination
	isRemote := strings.Contains(dest, ":")

	// Pre-flight: check destination is reachable
	if !isRemote {
		if !w.fileExist(dest) {
			msg := fmt.Sprintf("Backup failed: destination %s not found or not mounted", dest)
			w.publishFailure(hostname, msg, "Mount the backup drive or create the directory")
			w.persistState("failed", msg, "", "", now)
			return "❌ " + msg
		}
	} else {
		// For remote destinations, test SSH connectivity
		parts := strings.SplitN(dest, ":", 2)
		host := parts[0]
		_, err := w.runCmd("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", host, "true")
		if err != nil {
			msg := fmt.Sprintf("Backup failed: cannot reach remote host %s", host)
			w.publishFailure(hostname, msg, "Check SSH keys and connectivity to "+host)
			w.persistState("failed", msg, "", "", now)
			return "❌ " + msg
		}
	}

	// Build date-stamped destination directory
	dateDir := now.Format("2006-01-02")
	var fullDest string
	if isRemote {
		parts := strings.SplitN(dest, ":", 2)
		fullDest = parts[0] + ":" + filepath.Join(parts[1], dateDir) + "/"
	} else {
		fullDest = filepath.Join(dest, dateDir) + "/"
	}

	// Build rsync args
	args := w.buildRsyncArgs(fullDest, isRemote, dest, dateDir)

	// Run rsync
	slog.Info("backup: running rsync", "dest", fullDest, "sources", w.Cfg.Backup.Sources)
	output, err := w.runRsync(args...)
	duration := w.nowFunc().Sub(startTime)

	if err != nil {
		msg := fmt.Sprintf("Backup failed: rsync error")
		details := truncateStr(output, 500)
		w.publishFailure(hostname, msg, "Check rsync output and destination permissions")
		w.persistState("failed", msg, details, duration.String(), now)
		return fmt.Sprintf("❌ %s\n%s", msg, details)
	}

	// Update latest symlink (local only)
	if !isRemote {
		latestLink := filepath.Join(dest, "latest")
		os.Remove(latestLink)
		os.Symlink(filepath.Join(dest, dateDir), latestLink)
	}

	// Retention cleanup
	w.cleanupOldBackups(dest, isRemote)

	// Parse rsync output for transfer size
	size := parseRsyncSize(output)

	// Publish success event
	msg := fmt.Sprintf("Backup completed in %s", formatDuration(duration))
	if size != "" {
		msg += fmt.Sprintf(" (%s transferred)", size)
	}

	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("backup.completed-%d", w.nowFunc().UnixNano()),
		Type:      models.EventBackupCompleted,
		Severity:  models.SeverityInfo,
		Hostname:  hostname,
		Timestamp: w.nowFunc(),
		Message:   msg,
		Details:   truncateStr(output, 500),
		Source:    "backup",
	})

	w.persistState("success", "", size, duration.String(), now)

	slog.Info("backup: complete", "duration", duration, "size", size)
	return "✅ " + msg
}

func (w *BackupWatcher) buildRsyncArgs(fullDest string, isRemote bool, dest, dateDir string) []string {
	flags := w.Cfg.Backup.RsyncFlags
	if flags == "" {
		flags = "-avz --delete"
	}

	args := strings.Fields(flags)

	// Add SSH transport for remote destinations
	if isRemote {
		args = append(args, "-e", "ssh -o BatchMode=yes -o ConnectTimeout=10")
	}

	// Link-dest for incremental backups (local only — rsync handles remote link-dest)
	if !isRemote {
		latestLink := filepath.Join(dest, "latest")
		if w.fileExist(latestLink) {
			args = append(args, "--link-dest="+latestLink)
		}
	}

	// Add sources
	for _, src := range w.Cfg.Backup.Sources {
		if !strings.HasSuffix(src, "/") {
			src += "/"
		}
		args = append(args, src)
	}

	// Add destination
	args = append(args, fullDest)

	return args
}

func (w *BackupWatcher) cleanupOldBackups(dest string, isRemote bool) {
	retention := w.Cfg.Backup.Retention
	if retention <= 0 {
		return
	}

	if isRemote {
		// Remote cleanup: list dirs via SSH, remove oldest
		parts := strings.SplitN(dest, ":", 2)
		host := parts[0]
		remotePath := parts[1]

		out, err := w.runCmd("ssh", "-o", "BatchMode=yes", host, "ls", "-1", remotePath)
		if err != nil {
			slog.Warn("backup: failed to list remote backups for cleanup", "error", err)
			return
		}
		dirs := filterDateDirs(strings.Split(strings.TrimSpace(out), "\n"))
		if len(dirs) <= retention {
			return
		}
		sort.Strings(dirs)
		for _, d := range dirs[:len(dirs)-retention] {
			slog.Info("backup: removing old remote backup", "dir", d)
			w.runCmd("ssh", "-o", "BatchMode=yes", host, "rm", "-rf", filepath.Join(remotePath, d))
		}
	} else {
		entries, err := os.ReadDir(dest)
		if err != nil {
			return
		}
		var dirs []string
		for _, e := range entries {
			if e.IsDir() && isDateDir(e.Name()) {
				dirs = append(dirs, e.Name())
			}
		}
		if len(dirs) <= retention {
			return
		}
		sort.Strings(dirs)
		for _, d := range dirs[:len(dirs)-retention] {
			slog.Info("backup: removing old backup", "dir", d)
			os.RemoveAll(filepath.Join(dest, d))
		}
	}
}

func (w *BackupWatcher) publishFailure(hostname, msg, suggested string) {
	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("backup.failed-%d", w.nowFunc().UnixNano()),
		Type:      models.EventBackupFailed,
		Severity:  models.SeverityWarning,
		Hostname:  hostname,
		Timestamp: w.nowFunc(),
		Message:   msg,
		Suggested: suggested,
		Source:    "backup",
	})
}

func (w *BackupWatcher) persistState(status, lastError, size, duration string, t time.Time) {
	if w.store == nil {
		return
	}
	w.store.SetState("backup.last_status", status)
	w.store.SetState("backup.last_time", t.Format(time.RFC3339))
	w.store.SetState("backup.last_size", size)
	w.store.SetState("backup.last_duration", duration)
	w.store.SetState("backup.last_error", lastError)
}

// GetStatus returns a Telegram-friendly HTML status string.
func (w *BackupWatcher) GetStatus() string {
	if w.store == nil {
		return "❌ Backup status unavailable (no store)"
	}

	status, _ := w.store.GetState("backup.last_status")
	lastTime, _ := w.store.GetState("backup.last_time")
	size, _ := w.store.GetState("backup.last_size")
	duration, _ := w.store.GetState("backup.last_duration")
	lastError, _ := w.store.GetState("backup.last_error")

	if status == "" {
		return `💾 <b>Backup Status</b>

No backups have run yet.

<b>Config:</b>
  Sources: ` + strings.Join(w.Cfg.Backup.Sources, ", ") + `
  Destination: ` + w.Cfg.Backup.Destination + `
  Schedule: ` + w.Cfg.Backup.DayOfWeek + " @ " + w.Cfg.Backup.Time
	}

	icon := "✅"
	if status == "failed" {
		icon = "❌"
	}

	var b strings.Builder
	b.WriteString("💾 <b>Backup Status</b>\n\n")
	b.WriteString(fmt.Sprintf("  %s Last: %s\n", icon, status))

	if lastTime != "" {
		if t, err := time.Parse(time.RFC3339, lastTime); err == nil {
			b.WriteString(fmt.Sprintf("  🕐 When: %s\n", t.Format("2006-01-02 15:04")))
		}
	}
	if duration != "" {
		b.WriteString(fmt.Sprintf("  ⏱️ Duration: %s\n", duration))
	}
	if size != "" {
		b.WriteString(fmt.Sprintf("  📦 Transferred: %s\n", size))
	}
	if lastError != "" {
		b.WriteString(fmt.Sprintf("  ⚠️ Error: %s\n", lastError))
	}

	b.WriteString(fmt.Sprintf("\n<b>Config:</b>\n  Sources: %s\n  Dest: %s\n  Schedule: %s @ %s\n  Retention: %d",
		strings.Join(w.Cfg.Backup.Sources, ", "),
		w.Cfg.Backup.Destination,
		w.Cfg.Backup.DayOfWeek,
		w.Cfg.Backup.Time,
		w.Cfg.Backup.Retention,
	))

	return b.String()
}

// isDateDir checks if a directory name matches YYYY-MM-DD format.
func isDateDir(name string) bool {
	if len(name) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", name)
	return err == nil
}

// filterDateDirs returns only entries that look like YYYY-MM-DD.
func filterDateDirs(entries []string) []string {
	var result []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if isDateDir(e) {
			result = append(result, e)
		}
	}
	return result
}

// parseRsyncSize extracts the "total size" or "sent" bytes from rsync output.
func parseRsyncSize(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "total size is") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				return formatBytes(parts[3])
			}
		}
		if strings.HasPrefix(line, "sent") && strings.Contains(line, "bytes") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return formatBytes(parts[1])
			}
		}
	}
	return ""
}

// formatBytes converts a numeric string to a human-readable size.
func formatBytes(s string) string {
	s = strings.ReplaceAll(s, ",", "")
	var n float64
	if _, err := fmt.Sscanf(s, "%f", &n); err != nil {
		return s
	}
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", n/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", n/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", n/float64(1<<10))
	default:
		return fmt.Sprintf("%.0f B", n)
	}
}

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
