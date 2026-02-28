package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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

	// Telegram interactive bot (two-way commands)
	if cfg.Notifications.Telegram.Enabled {
		d.watchers = append(d.watchers, watchers.NewTelegramBotWatcher(cfg, bus, db))
	}
	if cfg.SecurityTools.Enabled {
		d.watchers = append(d.watchers, watchers.NewSecToolsWatcher(cfg, bus))
	}
	if cfg.Docker.Enabled {
		d.watchers = append(d.watchers, watchers.NewDockerWatcher(cfg, bus))
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
		_ = n.SendRaw(fmt.Sprintf("🛡️ <b>PiGuard started</b> on %s\nVersion %s | %d watchers | %d notifiers",
			hostname, Version, len(d.watchers), len(d.notifiers)))
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

	// Send to all notifiers
	for _, n := range d.notifiers {
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
				_ = n.SendRaw(msg)
			}

			// Sleep past this minute to avoid double-send
			time.Sleep(61 * time.Second)
		}
	}
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
