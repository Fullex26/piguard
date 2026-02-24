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

	hostname, _ := os.Hostname()
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		if !match(line) {
			continue
		}

		slog.Info("sectools: match found", "type", evType, "line", line)
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("%s-%d", string(evType), time.Now().UnixNano()),
			Type:      evType,
			Severity:  models.SeverityCritical,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   line,
			Details:   fmt.Sprintf("Log file: %s", path),
			Suggested: suggestedAction(evType),
			Source:    "sectools",
		})
	}

	// Store the new offset (position after last complete line).
	pos, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		w.offsets[path] = pos
	}
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
		return "Review rkhunter report: sudo rkhunter --report-warnings-only. Investigate flagged items."
	}
	return ""
}
