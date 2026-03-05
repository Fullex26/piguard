package watchers

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// ConnectivityWatcher polls a set of TCP hosts and fires events when
// connectivity is lost or restored.
type ConnectivityWatcher struct {
	Base
	interval    time.Duration
	hosts       []string
	outageStart time.Time         // zero value means currently connected
	dialFn      func(host string) bool // injectable for tests
}

func NewConnectivityWatcher(cfg *config.Config, bus *eventbus.Bus) *ConnectivityWatcher {
	interval, err := time.ParseDuration(cfg.Connectivity.PollInterval)
	if err != nil || interval <= 0 {
		interval = 30 * time.Second
	}

	hosts := cfg.Connectivity.Hosts
	if len(hosts) == 0 {
		hosts = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}

	w := &ConnectivityWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		interval: interval,
		hosts:    hosts,
	}
	w.dialFn = func(host string) bool {
		conn, err := net.DialTimeout("tcp", host, 3*time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}
	return w
}

func (w *ConnectivityWatcher) Name() string { return "connectivity" }
func (w *ConnectivityWatcher) Stop() error  { return nil }

func (w *ConnectivityWatcher) Start(ctx context.Context) error {
	slog.Info("starting connectivity watcher", "interval", w.interval, "hosts", w.hosts)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *ConnectivityWatcher) check() {
	reachable := false
	for _, host := range w.hosts {
		if w.dialFn(host) {
			reachable = true
			break
		}
	}

	hostname, _ := os.Hostname()

	if !reachable && w.outageStart.IsZero() {
		// Transition: connected → lost
		w.outageStart = time.Now()
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("connectivity.lost-%d", w.outageStart.UnixNano()),
			Type:      models.EventConnectivityLost,
			Severity:  models.SeverityCritical,
			Hostname:  hostname,
			Timestamp: w.outageStart,
			Message:   "Internet connectivity lost — all probe hosts unreachable",
			Details:   fmt.Sprintf("Probed hosts: %v", w.hosts),
			Suggested: "Check your router, ISP, or network interface",
			Source:    "connectivity",
		})
		return
	}

	if reachable && !w.outageStart.IsZero() {
		// Transition: lost → restored
		duration := time.Since(w.outageStart).Round(time.Second)
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("connectivity.restored-%d", time.Now().UnixNano()),
			Type:      models.EventConnectivityRestored,
			Severity:  models.SeverityInfo,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Internet connectivity restored (outage: %s)", duration),
			Details:   fmt.Sprintf("Probed hosts: %v", w.hosts),
			Source:    "connectivity",
		})
		w.outageStart = time.Time{}
	}
}
