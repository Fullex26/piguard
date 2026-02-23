package watchers

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/analysers"
	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// NetlinkWatcher monitors listening sockets using periodic ss polling
// with smart diffing. While true netlink SOCK_DIAG monitoring is possible,
// polling ss every 2 seconds is simpler, reliable, and still sub-second
// detection for our use case. We can upgrade to raw netlink later.
type NetlinkWatcher struct {
	Base
	labeller *analysers.PortLabeller
	baseline map[string]models.PortInfo // addr -> port info
	interval time.Duration
}

func NewNetlinkWatcher(cfg *config.Config, bus *eventbus.Bus) *NetlinkWatcher {
	return &NetlinkWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		labeller: analysers.NewPortLabeller(),
		baseline: make(map[string]models.PortInfo),
		interval: 2 * time.Second,
	}
}

func (w *NetlinkWatcher) Name() string { return "netlink" }

func (w *NetlinkWatcher) Start(ctx context.Context) error {
	slog.Info("starting port watcher", "interval", w.interval)

	// Initial scan to build baseline
	ports, err := w.scanPorts()
	if err != nil {
		return fmt.Errorf("initial port scan: %w", err)
	}

	for _, p := range ports {
		w.baseline[p.Address] = p
	}
	slog.Info("port baseline established", "count", len(w.baseline))

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

func (w *NetlinkWatcher) Stop() error { return nil }

func (w *NetlinkWatcher) check() {
	current, err := w.scanPorts()
	if err != nil {
		slog.Error("port scan failed", "error", err)
		return
	}

	currentMap := make(map[string]models.PortInfo)
	for _, p := range current {
		currentMap[p.Address] = p
	}

	// Detect new ports
	for addr, port := range currentMap {
		if _, exists := w.baseline[addr]; !exists {
			if w.isIgnored(addr) {
				continue
			}
			w.emitPortOpened(port)
		}
	}

	// Detect closed ports
	for addr, port := range w.baseline {
		if _, exists := currentMap[addr]; !exists {
			w.emitPortClosed(port)
		}
	}

	// Update baseline
	w.baseline = currentMap
}

func (w *NetlinkWatcher) scanPorts() ([]models.PortInfo, error) {
	// Use ss -tlnp for TCP listening sockets
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return nil, fmt.Errorf("running ss: %w", err)
	}

	var ports []models.PortInfo
	scanner := bufio.NewScanner(strings.NewReader(string(out)))

	// Skip header line
	if scanner.Scan() {
		// header consumed
	}

	for scanner.Scan() {
		line := scanner.Text()
		port, err := w.parseSsLine(line)
		if err != nil {
			continue // skip unparseable lines
		}
		// Enrich with process/container info
		port = w.labeller.Label(port)
		ports = append(ports, port)
	}

	return ports, nil
}

// parseSsLine parses a line from `ss -tlnp` output
// Format: State  Recv-Q  Send-Q  Local Address:Port  Peer Address:Port  Process
func (w *NetlinkWatcher) parseSsLine(line string) (models.PortInfo, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return models.PortInfo{}, fmt.Errorf("too few fields")
	}

	localAddr := fields[3]

	// Extract PID from process field if present
	pid := 0
	procName := ""
	if len(fields) >= 6 {
		procField := fields[5]
		// Format: users:(("name",pid=123,fd=4))
		if idx := strings.Index(procField, "pid="); idx >= 0 {
			pidStr := procField[idx+4:]
			if end := strings.IndexAny(pidStr, ",)"); end > 0 {
				pid, _ = strconv.Atoi(pidStr[:end])
			}
		}
		if idx := strings.Index(procField, "((\""); idx >= 0 {
			nameStr := procField[idx+3:]
			if end := strings.Index(nameStr, "\""); end > 0 {
				procName = nameStr[:end]
			}
		}
	}

	// Determine if exposed (bound to 0.0.0.0 or ::)
	isExposed := false
	host, _, err := net.SplitHostPort(localAddr)
	if err == nil {
		isExposed = host == "0.0.0.0" || host == "::" || host == "*"
	}

	return models.PortInfo{
		Address:     localAddr,
		Protocol:    "tcp",
		PID:         pid,
		ProcessName: procName,
		IsExposed:   isExposed,
	}, nil
}

func (w *NetlinkWatcher) isIgnored(addr string) bool {
	for _, pattern := range w.Cfg.Ports.Ignore {
		if matchAddrPattern(addr, pattern) {
			return true
		}
	}
	return false
}

// matchAddrPattern checks if an address matches a pattern like "127.0.0.1:*"
func matchAddrPattern(addr, pattern string) bool {
	if pattern == addr {
		return true
	}
	// Handle wildcard port
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(addr, prefix)
	}
	return false
}

func (w *NetlinkWatcher) emitPortOpened(port models.PortInfo) {
	severity := port.RiskLevel()

	msg := fmt.Sprintf("New listening port: %s → %s", port.Address, port.ProcessName)
	details := ""
	suggested := ""

	if port.ContainerName != "" {
		msg = fmt.Sprintf("New listening port: %s → %s (container: %s)",
			port.Address, port.ProcessName, port.ContainerName)
	}

	if port.IsExposed {
		details = "Bound to all interfaces — accessible from network"
		suggested = fmt.Sprintf("If this should be local-only, bind to 127.0.0.1 instead of 0.0.0.0")
		severity = models.SeverityWarning
	} else {
		details = "Localhost only — not network accessible ✓"
	}

	hostname, _ := os.Hostname()
	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("port-open-%s-%d", port.Address, time.Now().Unix()),
		Type:      models.EventPortOpened,
		Severity:  severity,
		Hostname:  hostname,
		Timestamp: time.Now(),
		Message:   msg,
		Details:   details,
		Suggested: suggested,
		Source:    "netlink",
		Port:     &port,
	})
}

func (w *NetlinkWatcher) emitPortClosed(port models.PortInfo) {
	hostname, _ := os.Hostname()
	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("port-close-%s-%d", port.Address, time.Now().Unix()),
		Type:      models.EventPortClosed,
		Severity:  models.SeverityInfo,
		Hostname:  hostname,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Port closed: %s → %s", port.Address, port.ProcessName),
		Source:    "netlink",
		Port:     &port,
	})
}
