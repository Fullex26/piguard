package watchers

import (
	"sync"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/analysers"
	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

func TestNetlinkWatcher_Name(t *testing.T) {
	w := &NetlinkWatcher{}
	if got := w.Name(); got != "netlink" {
		t.Errorf("Name() = %q, want %q", got, "netlink")
	}
}

func TestParseSsLine(t *testing.T) {
	w := &NetlinkWatcher{}
	tests := []struct {
		name        string
		line        string
		wantAddr    string
		wantPID     int
		wantProc    string
		wantExposed bool
		wantErr     bool
	}{
		{
			name:        "standard line with process",
			line:        `LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=1234,fd=3))`,
			wantAddr:    "0.0.0.0:22",
			wantPID:     1234,
			wantProc:    "sshd",
			wantExposed: true,
		},
		{
			name:        "localhost bind",
			line:        `LISTEN 0 128 127.0.0.1:8080 0.0.0.0:* users:(("node",pid=5678,fd=4))`,
			wantAddr:    "127.0.0.1:8080",
			wantPID:     5678,
			wantProc:    "node",
			wantExposed: false,
		},
		{
			name:        "IPv6 wildcard",
			line:        `LISTEN 0 128 :::443 :::* users:(("nginx",pid=999,fd=6))`,
			wantAddr:    ":::443",
			wantPID:     999,
			wantProc:    "nginx",
			wantExposed: true,
		},
		{
			name:        "missing process field",
			line:        `LISTEN 0 128 0.0.0.0:80 0.0.0.0:*`,
			wantAddr:    "0.0.0.0:80",
			wantPID:     0,
			wantProc:    "",
			wantExposed: true,
		},
		{
			name:    "too few fields",
			line:    `LISTEN 0`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := w.parseSsLine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if port.Address != tt.wantAddr {
				t.Errorf("Address = %q, want %q", port.Address, tt.wantAddr)
			}
			if port.PID != tt.wantPID {
				t.Errorf("PID = %d, want %d", port.PID, tt.wantPID)
			}
			if port.ProcessName != tt.wantProc {
				t.Errorf("ProcessName = %q, want %q", port.ProcessName, tt.wantProc)
			}
			if port.IsExposed != tt.wantExposed {
				t.Errorf("IsExposed = %v, want %v", port.IsExposed, tt.wantExposed)
			}
			if port.Protocol != "tcp" {
				t.Errorf("Protocol = %q, want %q", port.Protocol, "tcp")
			}
		})
	}
}

func TestMatchAddrPattern(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		pattern string
		want    bool
	}{
		{"exact match", "127.0.0.1:8080", "127.0.0.1:8080", true},
		{"wildcard port", "127.0.0.1:8080", "127.0.0.1:*", true},
		{"wildcard port different host", "0.0.0.0:8080", "127.0.0.1:*", false},
		{"no match", "192.168.1.1:22", "127.0.0.1:*", false},
		{"IPv6 wildcard", "::1:5432", "::1:*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchAddrPattern(tt.addr, tt.pattern); got != tt.want {
				t.Errorf("matchAddrPattern(%q, %q) = %v, want %v", tt.addr, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestNetlinkWatcher_ScanPorts_Injectable(t *testing.T) {
	cfg := &config.Config{
		Ports: config.PortConfig{Ignore: []string{"127.0.0.1:*"}},
	}
	w := &NetlinkWatcher{
		Base:     Base{Cfg: cfg, Bus: eventbus.New()},
		labeller: analysers.NewPortLabeller(),
		baseline: make(map[string]models.PortInfo),
		runSS: func() ([]byte, error) {
			return []byte("State  Recv-Q  Send-Q  Local Address:Port  Peer Address:Port  Process\n" +
				"LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((\"sshd\",pid=1234,fd=3))\n"), nil
		},
	}

	ports, err := w.scanPorts()
	if err != nil {
		t.Fatalf("scanPorts: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Address != "0.0.0.0:22" {
		t.Errorf("Address = %q, want %q", ports[0].Address, "0.0.0.0:22")
	}
}

func TestNetlinkWatcher_Check_NewPortPublishesEvent(t *testing.T) {
	bus := eventbus.New()
	var captured []models.Event
	var mu sync.Mutex
	bus.Subscribe(func(e models.Event) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
	})

	cfg := &config.Config{
		Ports: config.PortConfig{Ignore: []string{"127.0.0.1:*"}},
	}
	labeller := analysers.NewPortLabeller()
	labeller.ReadProcessNameFn(func(pid int) string { return "test" })

	w := &NetlinkWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		labeller: labeller,
		baseline: make(map[string]models.PortInfo),
		runSS: func() ([]byte, error) {
			return []byte("State  Recv-Q  Send-Q  Local Address:Port  Peer Address:Port  Process\n" +
				"LISTEN 0 128 0.0.0.0:8080 0.0.0.0:* users:((\"node\",pid=5678,fd=4))\n"), nil
		},
	}

	// baseline is empty, so 8080 should be "new"
	w.check()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, e := range captured {
		if e.Type == models.EventPortOpened {
			found = true
		}
	}
	if !found {
		t.Error("expected EventPortOpened for new port")
	}
}

func TestNetlinkWatcher_Check_ClosedPortPublishesEvent(t *testing.T) {
	bus := eventbus.New()
	var captured []models.Event
	var mu sync.Mutex
	bus.Subscribe(func(e models.Event) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
	})

	cfg := &config.Config{
		Ports: config.PortConfig{Ignore: []string{"127.0.0.1:*"}},
	}

	w := &NetlinkWatcher{
		Base: Base{Cfg: cfg, Bus: bus},
		labeller: analysers.NewPortLabeller(),
		baseline: map[string]models.PortInfo{
			"0.0.0.0:8080": {Address: "0.0.0.0:8080", ProcessName: "node"},
		},
		runSS: func() ([]byte, error) {
			// Return empty — port is gone
			return []byte("State  Recv-Q  Send-Q  Local Address:Port  Peer Address:Port  Process\n"), nil
		},
	}

	w.check()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, e := range captured {
		if e.Type == models.EventPortClosed {
			found = true
		}
	}
	if !found {
		t.Error("expected EventPortClosed for removed port")
	}
}

func TestNetlinkWatcher_Check_IgnoredPortSkipped(t *testing.T) {
	bus := eventbus.New()
	var captured []models.Event
	var mu sync.Mutex
	bus.Subscribe(func(e models.Event) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
	})

	cfg := &config.Config{
		Ports: config.PortConfig{Ignore: []string{"127.0.0.1:*"}},
	}

	w := &NetlinkWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		labeller: analysers.NewPortLabeller(),
		baseline: make(map[string]models.PortInfo),
		runSS: func() ([]byte, error) {
			return []byte("State  Recv-Q  Send-Q  Local Address:Port  Peer Address:Port  Process\n" +
				"LISTEN 0 128 127.0.0.1:8080 0.0.0.0:*\n"), nil
		},
	}

	w.check()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(captured) > 0 {
		t.Errorf("expected no events for ignored port, got %d", len(captured))
	}
}

func TestNetlinkWatcher_IsIgnored(t *testing.T) {
	cfg := &config.Config{
		Ports: config.PortConfig{
			Ignore: []string{"127.0.0.1:*", "::1:*"},
		},
	}
	w := &NetlinkWatcher{
		Base: Base{Cfg: cfg, Bus: eventbus.New()},
	}

	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"::1:5432", true},
		{"0.0.0.0:80", false},
		{"192.168.1.1:22", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := w.isIgnored(tt.addr); got != tt.want {
				t.Errorf("isIgnored(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}
