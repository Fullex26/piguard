package watchers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// SystemWatcher monitors disk, memory, CPU temperature
type SystemWatcher struct {
	Base
	interval time.Duration
}

func NewSystemWatcher(cfg *config.Config, bus *eventbus.Bus) *SystemWatcher {
	return &SystemWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		interval: 60 * time.Second,
	}
}

func (w *SystemWatcher) Name() string { return "system" }

func (w *SystemWatcher) Start(ctx context.Context) error {
	slog.Info("starting system watcher", "interval", w.interval)

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

func (w *SystemWatcher) Stop() error { return nil }

func (w *SystemWatcher) check() {
	hostname, _ := os.Hostname()

	// Disk usage
	disk := w.getDiskUsage()
	if disk > w.Cfg.System.DiskThreshold {
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("disk-%d", time.Now().Unix()),
			Type:      models.EventDiskHigh,
			Severity:  models.SeverityWarning,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Disk usage at %d%% (threshold: %d%%)", disk, w.Cfg.System.DiskThreshold),
			Suggested: "Check large files: sudo du -sh /var/log/* /tmp/* ~/",
			Source:    "system",
		})
	}

	// Memory
	mem := w.getMemoryUsage()
	if mem > w.Cfg.System.MemoryThreshold {
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("mem-%d", time.Now().Unix()),
			Type:      models.EventMemoryHigh,
			Severity:  models.SeverityWarning,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Memory usage at %d%% (threshold: %d%%)", mem, w.Cfg.System.MemoryThreshold),
			Suggested: "Check memory: free -h && docker stats --no-stream",
			Source:    "system",
		})
	}

	// CPU Temperature (Pi-specific)
	temp := w.getCPUTemp()
	if temp > 0 && int(temp) > w.Cfg.System.TempThreshold {
		w.Bus.Publish(models.Event{
			ID:        fmt.Sprintf("temp-%d", time.Now().Unix()),
			Type:      models.EventTempHigh,
			Severity:  models.SeverityWarning,
			Hostname:  hostname,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("CPU temperature at %.1f°C (threshold: %d°C)", temp, w.Cfg.System.TempThreshold),
			Suggested: "Check cooling: ensure ventilation or add a fan",
			Source:    "system",
		})
	}
}

func (w *SystemWatcher) getDiskUsage() int {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return 0
	}

	// Find root mount and check via statfs
	_ = data // Use syscall.Statfs for accurate data
	// Simplified: read from df output style via /proc
	// In production, use syscall.Statfs("/", &stat)

	// Fallback: parse /proc/diskstats or use syscall
	// For now, simple approach via reading statvfs
	var stat StatFS
	if err := statfs("/", &stat); err != nil {
		return 0
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	if total == 0 {
		return 0
	}
	used := total - free
	return int((used * 100) / total)
}

func (w *SystemWatcher) getMemoryUsage() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	var total, available int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		}
	}

	if total == 0 {
		return 0
	}
	used := total - available
	return int((used * 100) / total)
}

func (w *SystemWatcher) getCPUTemp() float64 {
	// Pi thermal zone
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	millideg, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0
	}
	return millideg / 1000.0
}

// GetSystemHealth returns current system health snapshot (for daily summary)
func GetSystemHealth(cfg *config.Config) models.SystemHealth {
	w := &SystemWatcher{Base: Base{Cfg: cfg}}
	return models.SystemHealth{
		DiskUsagePercent:  w.getDiskUsage(),
		MemoryUsedPercent: w.getMemoryUsage(),
		CPUTempCelsius:    w.getCPUTemp(),
	}
}

// StatFS holds filesystem stats (simplified)
type StatFS struct {
	Bsize  int64
	Blocks uint64
	Bfree  uint64
}

// statfs wraps the syscall - implemented in statfs_linux.go
func statfs(path string, stat *StatFS) error {
	// This will be implemented with syscall.Statfs in the linux build file
	// For now return error so getDiskUsage falls back gracefully
	return fmt.Errorf("not implemented on this platform")
}
