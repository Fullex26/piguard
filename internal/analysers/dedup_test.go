package analysers

import (
	"sync"
	"testing"
	"time"

	"github.com/Fullex26/piguard/pkg/models"
)

func TestNewDeduplicator(t *testing.T) {
	d := NewDeduplicator(time.Minute)
	if d == nil {
		t.Fatal("NewDeduplicator returned nil")
	}
}

func TestShouldAlert_FirstEvent(t *testing.T) {
	d := NewDeduplicator(time.Minute)
	e := models.Event{Type: models.EventPortOpened, Message: "new port"}
	if !d.ShouldAlert(e) {
		t.Error("first event should always alert")
	}
}

func TestShouldAlert_DuplicateWithinCooldown(t *testing.T) {
	d := NewDeduplicator(time.Second)
	e := models.Event{Type: models.EventPortOpened, Message: "new port"}

	if !d.ShouldAlert(e) {
		t.Error("first call should return true")
	}
	if d.ShouldAlert(e) {
		t.Error("second call within cooldown should return false")
	}
}

func TestShouldAlert_DuplicateAfterCooldown(t *testing.T) {
	d := NewDeduplicator(10 * time.Millisecond)
	e := models.Event{Type: models.EventPortOpened, Message: "new port"}

	if !d.ShouldAlert(e) {
		t.Error("first call should return true")
	}
	time.Sleep(20 * time.Millisecond)
	if !d.ShouldAlert(e) {
		t.Error("call after cooldown should return true")
	}
}

func TestShouldAlert_CriticalFirstAlerts_ThenDeduped(t *testing.T) {
	d := NewDeduplicator(time.Hour) // very long cooldown
	e := models.Event{
		Type:     models.EventFirewallChanged,
		Severity: models.SeverityCritical,
		Message:  "firewall breached",
	}

	// First occurrence must always get through regardless of severity.
	if !d.ShouldAlert(e) {
		t.Error("first critical event should alert")
	}
	// Subsequent identical criticals must be suppressed within the cooldown window.
	// This prevents persistent conditions (e.g. missing iptables rule) from flooding alerts.
	for i := range 4 {
		if d.ShouldAlert(e) {
			t.Errorf("repeated critical event iteration %d should be deduplicated within cooldown", i+1)
		}
	}
}

func TestShouldAlert_DifferentEventTypes(t *testing.T) {
	d := NewDeduplicator(time.Hour)
	e1 := models.Event{Type: models.EventPortOpened, Message: "same message"}
	e2 := models.Event{Type: models.EventPortClosed, Message: "same message"}

	if !d.ShouldAlert(e1) {
		t.Error("first event type should alert")
	}
	if !d.ShouldAlert(e2) {
		t.Error("different event type should alert independently")
	}
}

func TestShouldAlert_PortDedup(t *testing.T) {
	d := NewDeduplicator(time.Hour)

	e1 := models.Event{
		Type: models.EventPortOpened,
		Port: &models.PortInfo{Address: "0.0.0.0:8080"},
	}
	e2 := models.Event{
		Type: models.EventPortOpened,
		Port: &models.PortInfo{Address: "0.0.0.0:9090"},
	}

	if !d.ShouldAlert(e1) {
		t.Error("first port event should alert")
	}
	if d.ShouldAlert(e1) {
		t.Error("same port event should deduplicate")
	}
	if !d.ShouldAlert(e2) {
		t.Error("different port address should alert")
	}
}

func TestShouldAlert_PortDedup_NilPort(t *testing.T) {
	d := NewDeduplicator(time.Hour)
	e := models.Event{
		Type:    models.EventPortOpened,
		Port:    nil,
		Message: "unknown port",
	}

	if !d.ShouldAlert(e) {
		t.Error("first call should alert")
	}
	if d.ShouldAlert(e) {
		t.Error("nil port falls back to type:message dedup")
	}
}

func TestShouldAlert_FirewallDedup(t *testing.T) {
	d := NewDeduplicator(time.Hour)

	e1 := models.Event{
		Type:     models.EventFirewallChanged,
		Firewall: &models.FirewallState{Chain: "INPUT"},
	}
	e2 := models.Event{
		Type:     models.EventFirewallChanged,
		Firewall: &models.FirewallState{Chain: "DOCKER-USER"},
	}

	if !d.ShouldAlert(e1) {
		t.Error("first firewall event should alert")
	}
	if d.ShouldAlert(e1) {
		t.Error("same chain should deduplicate")
	}
	if !d.ShouldAlert(e2) {
		t.Error("different chain should alert")
	}
}

func TestShouldAlert_FirewallDedup_NilFirewall(t *testing.T) {
	d := NewDeduplicator(time.Hour)
	e := models.Event{
		Type:     models.EventFirewallChanged,
		Firewall: nil,
		Message:  "fw change",
	}

	if !d.ShouldAlert(e) {
		t.Error("first call should alert")
	}
	if d.ShouldAlert(e) {
		t.Error("nil firewall falls back to type:message dedup")
	}
}

func TestShouldAlert_DualStackDedup(t *testing.T) {
	d := NewDeduplicator(time.Hour)

	// Docker binds the same port on both IPv4 and IPv6 simultaneously.
	// Only the first should fire an alert; the second must be suppressed.
	ipv4 := models.Event{
		Type: models.EventPortOpened,
		Port: &models.PortInfo{Address: "0.0.0.0:3001", ProcessName: "docker-proxy", IsExposed: true},
	}
	ipv6 := models.Event{
		Type: models.EventPortOpened,
		Port: &models.PortInfo{Address: ":::3001", ProcessName: "docker-proxy", IsExposed: true},
	}

	if !d.ShouldAlert(ipv4) {
		t.Error("first (IPv4) port.opened should alert")
	}
	if d.ShouldAlert(ipv6) {
		t.Error("IPv6 dual-stack duplicate should be deduplicated")
	}
}

func TestShouldAlert_DualStackDedup_DifferentPorts(t *testing.T) {
	d := NewDeduplicator(time.Hour)

	p80 := models.Event{
		Type: models.EventPortOpened,
		Port: &models.PortInfo{Address: "0.0.0.0:80", ProcessName: "nginx", IsExposed: true},
	}
	p443 := models.Event{
		Type: models.EventPortOpened,
		Port: &models.PortInfo{Address: "0.0.0.0:443", ProcessName: "nginx", IsExposed: true},
	}

	// Two different ports from the same process must both alert.
	if !d.ShouldAlert(p80) {
		t.Error("port 80 should alert")
	}
	if !d.ShouldAlert(p443) {
		t.Error("port 443 should alert independently from port 80")
	}
}

func TestShouldAlert_GenericDedup(t *testing.T) {
	d := NewDeduplicator(time.Hour)
	e := models.Event{Type: models.EventDiskHigh, Message: "disk at 95%"}

	if !d.ShouldAlert(e) {
		t.Error("first call should alert")
	}
	if d.ShouldAlert(e) {
		t.Error("same type+message should deduplicate")
	}
}

func TestCleanup(t *testing.T) {
	d := NewDeduplicator(10 * time.Millisecond)
	e := models.Event{Type: models.EventDiskHigh, Message: "disk full"}

	d.ShouldAlert(e) // prime the entry
	time.Sleep(30 * time.Millisecond) // > cooldown*2
	d.Cleanup()

	if !d.ShouldAlert(e) {
		t.Error("after cleanup, event should alert again")
	}
}

func TestShouldAlert_Concurrent(t *testing.T) {
	d := NewDeduplicator(time.Second)
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			e := models.Event{Type: models.EventPortOpened, Message: "concurrent"}
			d.ShouldAlert(e)
		}(i)
	}

	wg.Wait()
}
