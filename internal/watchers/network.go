package watchers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

type networkDevice struct {
	IP  string
	MAC string
}

// NetworkScanWatcher monitors the local ARP neighbour table for unknown devices.
// It uses `ip neigh show` (iproute2) which requires no root and no extra packages.
type NetworkScanWatcher struct {
	Base
	interval   time.Duration
	alertLeave bool
	ignoreMACs map[string]bool          // lowercase MAC → true
	baseline   map[string]networkDevice // MAC → device
	runIPNeigh func() ([]byte, error)   // injectable for tests
}

func NewNetworkScanWatcher(cfg *config.Config, bus *eventbus.Bus) *NetworkScanWatcher {
	interval, err := time.ParseDuration(cfg.Network.PollInterval)
	if err != nil || interval <= 0 {
		interval = 5 * time.Minute
	}
	ignore := make(map[string]bool, len(cfg.Network.IgnoreMACs))
	for _, mac := range cfg.Network.IgnoreMACs {
		ignore[strings.ToLower(mac)] = true
	}
	w := &NetworkScanWatcher{
		Base:       Base{Cfg: cfg, Bus: bus},
		interval:   interval,
		alertLeave: cfg.Network.AlertOnLeave,
		ignoreMACs: ignore,
		baseline:   make(map[string]networkDevice),
	}
	w.runIPNeigh = func() ([]byte, error) {
		return exec.Command("ip", "neigh", "show").Output()
	}
	return w
}

func (w *NetworkScanWatcher) Name() string { return "network-scan" }
func (w *NetworkScanWatcher) Stop() error  { return nil }

func (w *NetworkScanWatcher) Start(ctx context.Context) error {
	slog.Info("starting network scan watcher", "interval", w.interval)

	// Build initial baseline silently (no alerts for pre-existing devices).
	if out, err := w.runIPNeigh(); err == nil {
		for _, d := range parseIPNeigh(string(out)) {
			w.baseline[d.MAC] = d
		}
		slog.Info("network baseline established", "count", len(w.baseline))
	} else {
		slog.Warn("ip neigh not available at startup", "error", err)
	}

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

func (w *NetworkScanWatcher) check() {
	out, err := w.runIPNeigh()
	if err != nil {
		slog.Debug("ip neigh check skipped", "error", err)
		return
	}

	current := make(map[string]networkDevice)
	for _, d := range parseIPNeigh(string(out)) {
		current[d.MAC] = d
	}

	hostname, _ := os.Hostname()

	// Detect new devices (unknown MAC).
	for mac, d := range current {
		if w.ignoreMACs[mac] {
			continue
		}
		if _, known := w.baseline[mac]; !known {
			w.Bus.Publish(models.Event{
				ID:        fmt.Sprintf("%s-%s-%d", string(models.EventNetworkNewDevice), mac, time.Now().UnixNano()),
				Type:      models.EventNetworkNewDevice,
				Severity:  models.SeverityInfo,
				Hostname:  hostname,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("New device on network: %s (%s)", d.IP, mac),
				Source:    "network-scan",
			})
		}
	}

	// Detect departed devices (opt-in — fire event and remove from baseline).
	if w.alertLeave {
		for mac, d := range w.baseline {
			if w.ignoreMACs[mac] {
				continue
			}
			if _, present := current[mac]; !present {
				w.Bus.Publish(models.Event{
					ID:        fmt.Sprintf("%s-%s-%d", string(models.EventNetworkDeviceLeft), mac, time.Now().UnixNano()),
					Type:      models.EventNetworkDeviceLeft,
					Severity:  models.SeverityInfo,
					Hostname:  hostname,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Device left network: %s (%s)", d.IP, mac),
					Source:    "network-scan",
				})
				delete(w.baseline, mac)
			}
		}
	}

	// Add / update all current devices into the baseline.
	// When alertLeave=false we never shrink the baseline: ARP entries age out for idle
	// devices (sleeping phones etc.), so keeping departed MACs prevents spurious
	// EventNetworkNewDevice alerts when their ARP entries reappear.
	for mac, d := range current {
		w.baseline[mac] = d
	}
}

// parseIPNeigh parses output from `ip neigh show` and returns devices with known MACs.
// Entries in FAILED or INCOMPLETE state are skipped (no device responded).
// MACs are normalised to lowercase.
func parseIPNeigh(output string) []networkDevice {
	var devices []networkDevice
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// State is the last field (REACHABLE, STALE, DELAY, PROBE, FAILED, INCOMPLETE, NOARP, PERMANENT).
		state := fields[len(fields)-1]
		if state == "FAILED" || state == "INCOMPLETE" {
			continue
		}
		// Find the "lladdr" keyword; the next field is the MAC address.
		lladdrIdx := -1
		for i, f := range fields {
			if f == "lladdr" {
				lladdrIdx = i
				break
			}
		}
		if lladdrIdx < 0 || lladdrIdx+1 >= len(fields) {
			continue // no MAC (e.g. directly-connected interface entry)
		}
		ip := fields[0]
		mac := strings.ToLower(fields[lladdrIdx+1])
		devices = append(devices, networkDevice{IP: ip, MAC: mac})
	}
	return devices
}
