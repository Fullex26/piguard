package watchers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

func TestParseSSHFailed(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantIP   string
		wantUser string
		wantOK   bool
	}{
		{
			"valid user",
			"Jun  5 10:15:30 pi sshd[1234]: Failed password for admin from 192.168.1.100 port 52341 ssh2",
			"192.168.1.100", "admin", true,
		},
		{
			"invalid user",
			"Jun  5 10:15:30 pi sshd[1234]: Failed password for invalid user root from 10.0.0.5 port 22 ssh2",
			"10.0.0.5", "root", true,
		},
		{
			"non-match",
			"Jun  5 10:15:30 pi sshd[1234]: Connection closed by 192.168.1.100 port 52341",
			"", "", false,
		},
		{
			"empty",
			"",
			"", "", false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, user, ok := ParseSSHFailed(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ParseSSHFailed() ok = %v, want %v", ok, tt.wantOK)
			}
			if ip != tt.wantIP {
				t.Errorf("ParseSSHFailed() ip = %q, want %q", ip, tt.wantIP)
			}
			if user != tt.wantUser {
				t.Errorf("ParseSSHFailed() user = %q, want %q", user, tt.wantUser)
			}
		})
	}
}

func TestIsSudoFailure(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			"sudo failure",
			"Jun  5 10:15:30 pi sudo: pam_unix(sudo:auth): authentication failure; logname=pi uid=1000 euid=0",
			true,
		},
		{
			"ssh failure",
			"Jun  5 10:15:30 pi sshd[1234]: Failed password for root from 10.0.0.1 port 22 ssh2",
			false,
		},
		{
			"empty",
			"",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSudoFailure(tt.line); got != tt.want {
				t.Errorf("IsSudoFailure() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSSHLogin(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantIP   string
		wantUser string
		wantOK   bool
	}{
		{
			"publickey",
			"Jun  5 10:15:30 pi sshd[1234]: Accepted publickey for pi from 192.168.1.50 port 52341 ssh2",
			"192.168.1.50", "pi", true,
		},
		{
			"password",
			"Jun  5 10:15:30 pi sshd[1234]: Accepted password for admin from 10.0.0.1 port 22 ssh2",
			"10.0.0.1", "admin", true,
		},
		{
			"keyboard-interactive",
			"Jun  5 10:15:30 pi sshd[1234]: Accepted keyboard-interactive for user1 from 172.16.0.1 port 2222 ssh2",
			"172.16.0.1", "user1", true,
		},
		{
			"non-match",
			"Jun  5 10:15:30 pi sshd[1234]: Failed password for root from 10.0.0.1 port 22 ssh2",
			"", "", false,
		},
		{
			"empty",
			"",
			"", "", false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, user, ok := ParseSSHLogin(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ParseSSHLogin() ok = %v, want %v", ok, tt.wantOK)
			}
			if ip != tt.wantIP {
				t.Errorf("ParseSSHLogin() ip = %q, want %q", ip, tt.wantIP)
			}
			if user != tt.wantUser {
				t.Errorf("ParseSSHLogin() user = %q, want %q", user, tt.wantUser)
			}
		})
	}
}

func newTestAuthLogWatcher(t *testing.T, logPath string, threshold int) (*AuthLogWatcher, *eventbus.Bus) {
	t.Helper()
	bus := eventbus.New()
	cfg := config.DefaultConfig()
	cfg.AuthLog.Enabled = true
	cfg.AuthLog.LogPath = logPath
	cfg.AuthLog.BruteForceThreshold = threshold
	cfg.AuthLog.BruteForceWindow = "5m"
	cfg.AuthLog.PollInterval = "1s"

	w := NewAuthLogWatcher(cfg, bus)
	return w, bus
}

func TestBruteForceDetection(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "auth.log")

	w, bus := newTestAuthLogWatcher(t, logPath, 5)

	events := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { events <- e })

	// Write file with initial content (watcher starts at end)
	if err := os.WriteFile(logPath, []byte("initial line\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w.offset = 0 // Force reading from start for test

	// Write 5 failed attempts from same IP
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		f.WriteString("Jun  5 10:15:30 pi sshd[1234]: Failed password for root from 10.0.0.99 port 22 ssh2\n")
	}
	f.Close()

	w.scanLog()

	select {
	case e := <-events:
		if e.Type != models.EventSSHBruteForce {
			t.Errorf("expected EventSSHBruteForce, got %s", e.Type)
		}
		if e.Severity != models.SeverityCritical {
			t.Errorf("expected Critical severity, got %s", e.Severity.String())
		}
	case <-time.After(time.Second):
		t.Error("expected brute force event, got none")
	}
}

func TestBruteForceDetection_BelowThreshold(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "auth.log")

	w, bus := newTestAuthLogWatcher(t, logPath, 5)

	events := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { events <- e })

	// Write 4 failed attempts (below threshold of 5)
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		f.WriteString("Jun  5 10:15:30 pi sshd[1234]: Failed password for root from 10.0.0.99 port 22 ssh2\n")
	}
	f.Close()

	w.offset = 0
	w.scanLog()

	select {
	case e := <-events:
		t.Errorf("expected no event, got %s", e.Type)
	case <-time.After(100 * time.Millisecond):
		// Expected: no event
	}
}

func TestBruteForce_DifferentIPs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "auth.log")

	w, bus := newTestAuthLogWatcher(t, logPath, 5)

	events := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { events <- e })

	// 3 from IP-A + 3 from IP-B: neither should trigger (threshold=5)
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		f.WriteString("Jun  5 10:15:30 pi sshd[1234]: Failed password for root from 10.0.0.1 port 22 ssh2\n")
		f.WriteString("Jun  5 10:15:30 pi sshd[1234]: Failed password for root from 10.0.0.2 port 22 ssh2\n")
	}
	f.Close()

	w.offset = 0
	w.scanLog()

	select {
	case e := <-events:
		t.Errorf("expected no event with different IPs, got %s", e.Type)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestSudoFailureDetection(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "auth.log")

	w, bus := newTestAuthLogWatcher(t, logPath, 5)

	events := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { events <- e })

	if err := os.WriteFile(logPath, []byte(
		"Jun  5 10:15:30 pi sudo: pam_unix(sudo:auth): authentication failure; logname=pi uid=1000 euid=0\n",
	), 0644); err != nil {
		t.Fatal(err)
	}

	w.offset = 0
	w.scanLog()

	select {
	case e := <-events:
		if e.Type != models.EventSudoFailure {
			t.Errorf("expected EventSudoFailure, got %s", e.Type)
		}
		if e.Severity != models.SeverityWarning {
			t.Errorf("expected Warning severity, got %s", e.Severity.String())
		}
	case <-time.After(time.Second):
		t.Error("expected sudo failure event, got none")
	}
}

func TestLogRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "auth.log")

	w, _ := newTestAuthLogWatcher(t, logPath, 5)

	// Simulate: watcher was at offset 10000, but file is now smaller
	if err := os.WriteFile(logPath, []byte("short content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w.offset = 10000

	// Should not panic; should reset offset
	w.scanLog()

	if w.offset == 10000 {
		t.Error("expected offset to reset after rotation")
	}
}

func TestMissingFile(t *testing.T) {
	w, _ := newTestAuthLogWatcher(t, "/nonexistent/auth.log", 5)
	// Should not panic
	w.scanLog()
}
