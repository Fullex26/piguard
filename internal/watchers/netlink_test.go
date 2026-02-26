package watchers

import (
	"testing"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
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
