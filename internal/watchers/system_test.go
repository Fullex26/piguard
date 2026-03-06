package watchers

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

func newTestSystemWatcher(cfg *config.Config) (*SystemWatcher, *eventCapture) {
	bus := eventbus.New()
	cap := &eventCapture{}
	bus.Subscribe(func(e models.Event) {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		cap.events = append(cap.events, e)
	})
	w := NewSystemWatcher(cfg, bus)
	return w, cap
}

type eventCapture struct {
	mu     sync.Mutex
	events []models.Event
}

func (c *eventCapture) Events() []models.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]models.Event, len(c.events))
	copy(cp, c.events)
	return cp
}

func TestSystemWatcher_Name(t *testing.T) {
	w := &SystemWatcher{}
	if got := w.Name(); got != "system" {
		t.Errorf("Name() = %q, want %q", got, "system")
	}
}

func TestSystemWatcher_MemoryParsing_Normal(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readMemInfo = func() ([]byte, error) {
		return []byte("MemTotal:        8000000 kB\nMemFree:         1000000 kB\nMemAvailable:    4000000 kB\n"), nil
	}

	mem := w.getMemoryUsage()
	// used = 8000000 - 4000000 = 4000000, percent = 4000000*100/8000000 = 50
	if mem != 50 {
		t.Errorf("getMemoryUsage() = %d, want 50", mem)
	}
}

func TestSystemWatcher_MemoryParsing_HighUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readMemInfo = func() ([]byte, error) {
		return []byte("MemTotal:        1000000 kB\nMemFree:          50000 kB\nMemAvailable:     50000 kB\n"), nil
	}

	mem := w.getMemoryUsage()
	// used = 1000000 - 50000 = 950000, percent = 95
	if mem != 95 {
		t.Errorf("getMemoryUsage() = %d, want 95", mem)
	}
}

func TestSystemWatcher_MemoryParsing_ReadError(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readMemInfo = func() ([]byte, error) {
		return nil, fmt.Errorf("file not found")
	}

	if mem := w.getMemoryUsage(); mem != 0 {
		t.Errorf("getMemoryUsage() = %d, want 0 on error", mem)
	}
}

func TestSystemWatcher_MemoryParsing_ZeroTotal(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readMemInfo = func() ([]byte, error) {
		return []byte("MemTotal:        0 kB\nMemAvailable:    0 kB\n"), nil
	}

	if mem := w.getMemoryUsage(); mem != 0 {
		t.Errorf("getMemoryUsage() = %d, want 0 for zero total", mem)
	}
}

func TestSystemWatcher_MemoryParsing_Malformed(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readMemInfo = func() ([]byte, error) {
		return []byte("This is not meminfo\nJust garbage\n"), nil
	}

	if mem := w.getMemoryUsage(); mem != 0 {
		t.Errorf("getMemoryUsage() = %d, want 0 for malformed input", mem)
	}
}

func TestSystemWatcher_CPUTemp_Normal(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readCPUTemp = func() ([]byte, error) {
		return []byte("55000\n"), nil
	}

	temp := w.getCPUTemp()
	if temp != 55.0 {
		t.Errorf("getCPUTemp() = %f, want 55.0", temp)
	}
}

func TestSystemWatcher_CPUTemp_ReadError(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readCPUTemp = func() ([]byte, error) {
		return nil, fmt.Errorf("no thermal zone")
	}

	if temp := w.getCPUTemp(); temp != 0 {
		t.Errorf("getCPUTemp() = %f, want 0 on error", temp)
	}
}

func TestSystemWatcher_CPUTemp_Malformed(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.readCPUTemp = func() ([]byte, error) {
		return []byte("not-a-number\n"), nil
	}

	if temp := w.getCPUTemp(); temp != 0 {
		t.Errorf("getCPUTemp() = %f, want 0 for malformed", temp)
	}
}

func TestSystemWatcher_DiskUsage_Normal(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.statfsFunc = func(path string, stat *StatFS) error {
		stat.Bsize = 4096
		stat.Blocks = 1000
		stat.Bfree = 200
		return nil
	}

	disk := w.getDiskUsage()
	// used = (1000-200)*4096, total = 1000*4096, percent = 800/1000*100 = 80
	if disk != 80 {
		t.Errorf("getDiskUsage() = %d, want 80", disk)
	}
}

func TestSystemWatcher_DiskUsage_Error(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.statfsFunc = func(path string, stat *StatFS) error {
		return fmt.Errorf("not supported")
	}

	if disk := w.getDiskUsage(); disk != 0 {
		t.Errorf("getDiskUsage() = %d, want 0 on error", disk)
	}
}

func TestSystemWatcher_DiskUsage_ZeroBlocks(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestSystemWatcher(cfg)
	w.statfsFunc = func(path string, stat *StatFS) error {
		stat.Bsize = 4096
		stat.Blocks = 0
		stat.Bfree = 0
		return nil
	}

	if disk := w.getDiskUsage(); disk != 0 {
		t.Errorf("getDiskUsage() = %d, want 0 for zero blocks", disk)
	}
}

func TestSystemWatcher_Check_PublishesMemoryEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.System.MemoryThreshold = 50
	cfg.System.TempThreshold = 100 // won't trigger
	w, cap := newTestSystemWatcher(cfg)

	w.readMemInfo = func() ([]byte, error) {
		return []byte("MemTotal:        1000000 kB\nMemAvailable:    100000 kB\n"), nil
	}
	w.readCPUTemp = func() ([]byte, error) { return nil, fmt.Errorf("no zone") }
	w.statfsFunc = func(path string, stat *StatFS) error { return fmt.Errorf("no fs") }

	w.check()
	time.Sleep(50 * time.Millisecond) // bus delivers async

	events := cap.Events()
	found := false
	for _, e := range events {
		if e.Type == models.EventMemoryHigh {
			found = true
		}
	}
	if !found {
		t.Error("expected EventMemoryHigh to be published")
	}
}

func TestSystemWatcher_Check_PublishesTempEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.System.TempThreshold = 70
	cfg.System.MemoryThreshold = 100 // won't trigger
	w, cap := newTestSystemWatcher(cfg)

	w.readMemInfo = func() ([]byte, error) { return nil, fmt.Errorf("err") }
	w.readCPUTemp = func() ([]byte, error) { return []byte("75000\n"), nil }
	w.statfsFunc = func(path string, stat *StatFS) error { return fmt.Errorf("no fs") }

	w.check()
	time.Sleep(50 * time.Millisecond)

	events := cap.Events()
	found := false
	for _, e := range events {
		if e.Type == models.EventTempHigh {
			found = true
		}
	}
	if !found {
		t.Error("expected EventTempHigh to be published")
	}
}

func TestSystemWatcher_Check_NothingBelowThreshold(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.System.DiskThreshold = 90
	cfg.System.MemoryThreshold = 90
	cfg.System.TempThreshold = 80
	w, cap := newTestSystemWatcher(cfg)

	w.readMemInfo = func() ([]byte, error) {
		return []byte("MemTotal:        1000000 kB\nMemAvailable:    500000 kB\n"), nil
	}
	w.readCPUTemp = func() ([]byte, error) { return []byte("50000\n"), nil }
	w.statfsFunc = func(path string, stat *StatFS) error {
		stat.Bsize = 4096
		stat.Blocks = 1000
		stat.Bfree = 500
		return nil
	}

	w.check()
	time.Sleep(50 * time.Millisecond)

	if len(cap.Events()) > 0 {
		t.Errorf("expected no events below threshold, got %d", len(cap.Events()))
	}
}

func TestSystemWatcher_Check_PublishesDiskEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.System.DiskThreshold = 50
	cfg.System.MemoryThreshold = 100
	cfg.System.TempThreshold = 100
	w, cap := newTestSystemWatcher(cfg)

	w.readMemInfo = func() ([]byte, error) { return nil, fmt.Errorf("err") }
	w.readCPUTemp = func() ([]byte, error) { return nil, fmt.Errorf("err") }
	w.statfsFunc = func(path string, stat *StatFS) error {
		stat.Bsize = 4096
		stat.Blocks = 1000
		stat.Bfree = 100 // 90% used
		return nil
	}

	w.check()
	time.Sleep(50 * time.Millisecond)

	events := cap.Events()
	found := false
	for _, e := range events {
		if e.Type == models.EventDiskHigh {
			found = true
		}
	}
	if !found {
		t.Error("expected EventDiskHigh to be published")
	}
}
