package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Fullex26/piguard/internal/analysers"
	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/notifiers"
	"github.com/Fullex26/piguard/internal/store"
	"github.com/Fullex26/piguard/internal/watchers"
	"github.com/Fullex26/piguard/pkg/models"
)

// Version is set at build time via ldflags: -X github.com/Fullex26/piguard/internal/daemon.Version=<tag>
var Version = "dev"

// Daemon is the main PiGuard process
type Daemon struct {
	cfg       *config.Config
	bus       *eventbus.Bus
	store     *store.Store
	watchers  []watchers.Watcher
	notifiers []notifiers.Notifier
	dedup     *analysers.Deduplicator
}

// New creates a new daemon instance
func New(cfg *config.Config) (*Daemon, error) {
	bus := eventbus.New()

	// Open event store
	if err := os.MkdirAll("/var/lib/piguard", 0750); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}

	db, err := store.Open(store.DefaultDBPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}

	// Parse cooldown
	cooldown, err := time.ParseDuration(cfg.Ports.Cooldown)
	if err != nil {
		cooldown = 15 * time.Minute
	}

	d := &Daemon{
		cfg:   cfg,
		bus:   bus,
		store: db,
		dedup: analysers.NewDeduplicator(cooldown),
	}

	// Register watchers
	if cfg.Ports.Enabled {
		d.watchers = append(d.watchers, watchers.NewNetlinkWatcher(cfg, bus))
	}
	if cfg.Firewall.Enabled {
		d.watchers = append(d.watchers, watchers.NewFirewallWatcher(cfg, bus))
	}
	d.watchers = append(d.watchers, watchers.NewSystemWatcher(cfg, bus))

	if cfg.FileIntegrity.Enabled {
		d.watchers = append(d.watchers, watchers.NewInotifyWatcher(cfg, bus))
	}

	// Backup watcher (must be created before Telegram bot so it can be wired in)
	var backupW *watchers.BackupWatcher
	if cfg.Backup.Enabled {
		backupW = watchers.NewBackupWatcher(cfg, bus, db)
		d.watchers = append(d.watchers, backupW)
	}

	// Telegram interactive bot (two-way commands)
	if cfg.Notifications.Telegram.Enabled {
		tbot := watchers.NewTelegramBotWatcher(cfg, bus, db)
		tbot.BackupWatcher = backupW // nil-safe; commands check for nil
		d.watchers = append(d.watchers, tbot)
	}
	if cfg.SecurityTools.Enabled {
		d.watchers = append(d.watchers, watchers.NewSecToolsWatcher(cfg, bus))
	}
	if cfg.Docker.Enabled {
		d.watchers = append(d.watchers, watchers.NewDockerWatcher(cfg, bus))
	}
	if cfg.Network.Enabled {
		d.watchers = append(d.watchers, watchers.NewNetworkScanWatcher(cfg, bus))
	}
	if cfg.Connectivity.Enabled {
		d.watchers = append(d.watchers, watchers.NewConnectivityWatcher(cfg, bus))
	}
	if cfg.AutoUpdate.Enabled {
		d.watchers = append(d.watchers, watchers.NewAutoUpdateWatcher(cfg, bus))
	}
	if cfg.AuthLog.Enabled {
		d.watchers = append(d.watchers, watchers.NewAuthLogWatcher(cfg, bus))
	}

	// Register notifiers
	if cfg.Notifications.Telegram.Enabled {
		d.notifiers = append(d.notifiers, notifiers.NewTelegram(cfg.Notifications.Telegram))
	}
	if cfg.Notifications.Ntfy.Enabled {
		d.notifiers = append(d.notifiers, notifiers.NewNtfy(cfg.Notifications.Ntfy))
	}
	if cfg.Notifications.Discord.Enabled {
		d.notifiers = append(d.notifiers, notifiers.NewDiscord(cfg.Notifications.Discord))
	}
	if cfg.Notifications.Webhook.Enabled {
		d.notifiers = append(d.notifiers, notifiers.NewWebhook(cfg.Notifications.Webhook))
	}

	return d, nil
}

// Run starts the daemon and blocks until interrupted
func (d *Daemon) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Subscribe to events on the bus
	d.bus.Subscribe(func(event models.Event) {
		d.handleEvent(event)
	})

	// Start all watchers
	var wg sync.WaitGroup
	for _, w := range d.watchers {
		wg.Add(1)
		go func(w watchers.Watcher) {
			defer wg.Done()
			slog.Info("starting watcher", "name", w.Name())
			if err := w.Start(ctx); err != nil {
				slog.Error("watcher failed", "name", w.Name(), "error", err)
			}
		}(w)
	}

	// Start daily summary scheduler
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runDailySummary(ctx)
	}()

	// Start weekly report scheduler
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runWeeklyReport(ctx)
	}()

	// Start dedup cleanup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runCleanup(ctx)
	}()

	hostname, _ := os.Hostname()
	slog.Info("piguard started",
		"version", Version,
		"hostname", hostname,
		"watchers", len(d.watchers),
		"notifiers", len(d.notifiers),
	)

	// Startup notification
	for _, n := range d.notifiers {
		msg := fmt.Sprintf("🛡️ <b>PiGuard started</b> on %s\nVersion %s | %d watchers | %d notifiers",
			hostname, Version, len(d.watchers), len(d.notifiers))
		slog.Info("sending notification", "notifier", n.Name(), "type", "startup")
		if err := n.SendRaw(msg); err != nil {
			slog.Error("notification failed", "notifier", n.Name(), "type", "startup", "error", err)
		}
	}

	// Wait for shutdown signal
	<-sigCh
	slog.Info("shutting down...")
	cancel()
	wg.Wait()

	// Cleanup
	for _, w := range d.watchers {
		_ = w.Stop()
	}
	_ = d.store.Close()

	slog.Info("piguard stopped")
	return nil
}

func (d *Daemon) handleEvent(event models.Event) {
	// Save to store
	if err := d.store.SaveEvent(event); err != nil {
		slog.Error("failed to save event", "error", err)
	}

	// Check dedup
	if !d.dedup.ShouldAlert(event) {
		slog.Debug("event deduplicated", "type", event.Type, "message", event.Message)
		return
	}

	// Quiet hours: suppress non-critical notifications (events still saved above)
	if d.isQuietHour(time.Now()) && event.Severity != models.SeverityCritical {
		slog.Debug("quiet hours: suppressing notification", "type", event.Type, "severity", event.Severity.String())
		return
	}

	// Send to all notifiers
	for _, n := range d.notifiers {
		slog.Info("sending notification",
			"notifier", n.Name(),
			"type", string(event.Type),
			"severity", event.Severity.String(),
			"message", event.Message,
		)
		if err := n.Send(event); err != nil {
			slog.Error("notification failed", "notifier", n.Name(), "error", err)
		}
	}
}

func (d *Daemon) runDailySummary(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
			now := time.Now()
			summary := d.cfg.Alerts.DailySummary
			if summary == "" {
				continue
			}

			target := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
			if target != summary {
				continue
			}

			hostname, _ := os.Hostname()
			health := watchers.GetSystemHealth(d.cfg)
			lastAlert, _ := d.store.GetLastAlertTime()

			msg := notifiers.FormatDailySummary(hostname, health, lastAlert)
			for _, n := range d.notifiers {
				slog.Info("sending notification", "notifier", n.Name(), "type", "daily_summary")
				if err := n.SendRaw(msg); err != nil {
					slog.Error("notification failed", "notifier", n.Name(), "type", "daily_summary", "error", err)
				}
			}

			// Sleep past this minute to avoid double-send
			time.Sleep(61 * time.Second)
		}
	}
}

func (d *Daemon) runWeeklyReport(ctx context.Context) {
	schedule := d.cfg.Alerts.WeeklyReport
	if schedule == "" {
		return
	}

	// Parse "sunday:20:00" → weekday + HH:MM
	parts := strings.SplitN(schedule, ":", 2)
	if len(parts) != 2 {
		slog.Warn("invalid weekly_report format", "value", schedule)
		return
	}
	weekday := parseWeekdayName(parts[0])
	timeStr := parts[1]

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
			now := time.Now()
			if now.Weekday() != weekday {
				continue
			}
			hhmm := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
			if hhmm != timeStr {
				continue
			}

			hostname, _ := os.Hostname()
			thisWeek, _ := d.store.GetEventCountByType(7)
			lastWeek, _ := d.store.GetEventCountByType(14)

			// Subtract this week's counts from the 14-day total to get last week only
			lastWeekOnly := make(map[string]int)
			for k, v := range lastWeek {
				lastWeekOnly[k] = v - thisWeek[k]
			}

			totalThis := 0
			for _, v := range thisWeek {
				totalThis += v
			}
			totalLast := 0
			for _, v := range lastWeekOnly {
				totalLast += v
			}

			uptimeStr := getUptimeStr()
			msg := notifiers.FormatWeeklyReport(hostname, thisWeek, lastWeekOnly, totalThis, totalLast, uptimeStr)
			for _, n := range d.notifiers {
				slog.Info("sending notification", "notifier", n.Name(), "type", "weekly_report")
				if err := n.SendRaw(msg); err != nil {
					slog.Error("notification failed", "notifier", n.Name(), "type", "weekly_report", "error", err)
				}
			}

			// Sleep past this minute to avoid double-send
			time.Sleep(61 * time.Second)
		}
	}
}

func parseWeekdayName(s string) time.Weekday {
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

func getUptimeStr() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "unknown"
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	days := int(seconds) / 86400
	hours := (int(seconds) % 86400) / 3600
	return fmt.Sprintf("%dd %dh", days, hours)
}

func (d *Daemon) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.dedup.Cleanup()
			// Prune events older than 30 days
			pruned, _ := d.store.Prune(30)
			if pruned > 0 {
				slog.Info("pruned old events", "count", pruned)
			}
		}
	}
}

// parseHHMM parses "HH:MM" into hours and minutes.
func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid HH:MM format: %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour: %q", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute: %q", parts[1])
	}
	return h, m, nil
}

// isQuietHour returns true if the given time falls within the configured quiet window.
// Handles overnight wrap-around (e.g. 23:00–07:00) and same-day windows (e.g. 09:00–17:00).
// Returns false if quiet hours are not configured or invalid.
func (d *Daemon) isQuietHour(now time.Time) bool {
	qh := d.cfg.Alerts.QuietHours
	if qh.Start == "" || qh.End == "" {
		return false
	}

	sh, sm, err := parseHHMM(qh.Start)
	if err != nil {
		return false
	}
	eh, em, err := parseHHMM(qh.End)
	if err != nil {
		return false
	}

	startMin := sh*60 + sm
	endMin := eh*60 + em
	nowMin := now.Hour()*60 + now.Minute()

	if startMin <= endMin {
		// Same-day window: e.g. 09:00–17:00
		return nowMin >= startMin && nowMin < endMin
	}
	// Overnight wrap: e.g. 23:00–07:00
	return nowMin >= startMin || nowMin < endMin
}

// TestNotifiers sends a test message to all configured notifiers
func (d *Daemon) TestNotifiers() error {
	for _, n := range d.notifiers {
		slog.Info("testing notifier", "name", n.Name())
		if err := n.Test(); err != nil {
			return fmt.Errorf("%s: %w", n.Name(), err)
		}
		slog.Info("notifier OK", "name", n.Name())
	}
	return nil
}
