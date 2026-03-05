package watchers

import (
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

func newTestConnectivityWatcher(hosts []string, dialFn func(host string) bool) (*ConnectivityWatcher, chan models.Event) {
	bus := eventbus.New()
	received := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { received <- e })

	cfg := &config.Config{
		Connectivity: config.ConnectivityConfig{
			Enabled:      true,
			PollInterval: "30s",
			Hosts:        hosts,
		},
	}
	w := NewConnectivityWatcher(cfg, bus)
	w.dialFn = dialFn
	return w, received
}

func TestConnectivityWatcher_Name(t *testing.T) {
	cfg := &config.Config{
		Connectivity: config.ConnectivityConfig{PollInterval: "30s", Hosts: []string{"8.8.8.8:53"}},
	}
	w := NewConnectivityWatcher(cfg, eventbus.New())
	if got := w.Name(); got != "connectivity" {
		t.Errorf("Name() = %q, want %q", got, "connectivity")
	}
}

func TestConnectivityWatcher_SteadyConnected_NoEvent(t *testing.T) {
	w, received := newTestConnectivityWatcher([]string{"8.8.8.8:53"}, func(_ string) bool { return true })

	w.check()
	expectNoEvent(t, received)
}

func TestConnectivityWatcher_AllHostsFail_LostFired(t *testing.T) {
	w, received := newTestConnectivityWatcher([]string{"8.8.8.8:53", "1.1.1.1:53"}, func(_ string) bool { return false })

	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventConnectivityLost {
		t.Errorf("event type = %q, want %q", e.Type, models.EventConnectivityLost)
	}
	if e.Severity != models.SeverityCritical {
		t.Errorf("severity = %v, want Critical", e.Severity)
	}
	if w.outageStart.IsZero() {
		t.Error("outageStart should be set after lost event")
	}
}

func TestConnectivityWatcher_SecondFailDuringOutage_NoDuplicate(t *testing.T) {
	w, received := newTestConnectivityWatcher([]string{"8.8.8.8:53"}, func(_ string) bool { return false })

	// First check triggers the lost event and sets outageStart.
	w.check()
	awaitEvent(t, received)

	// Second check while still failing — no additional event.
	w.check()
	expectNoEvent(t, received)
}

func TestConnectivityWatcher_Recovery_RestoredFiredWithDuration(t *testing.T) {
	dialResult := false
	w, received := newTestConnectivityWatcher([]string{"8.8.8.8:53"}, func(_ string) bool { return dialResult })

	// Simulate outage.
	w.check()
	awaitEvent(t, received) // consume lost event

	// Simulate recovery.
	dialResult = true
	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventConnectivityRestored {
		t.Errorf("event type = %q, want %q", e.Type, models.EventConnectivityRestored)
	}
	if e.Severity != models.SeverityInfo {
		t.Errorf("severity = %v, want Info", e.Severity)
	}
	if !containsString(e.Message, "outage:") {
		t.Errorf("expected outage duration in message, got: %q", e.Message)
	}
	if !w.outageStart.IsZero() {
		t.Error("outageStart should be reset after restored event")
	}
}

func TestConnectivityWatcher_OneHostSuffices_NoOutage(t *testing.T) {
	calls := 0
	// First host fails, second succeeds.
	w, received := newTestConnectivityWatcher([]string{"bad:53", "good:53"}, func(host string) bool {
		calls++
		return host == "good:53"
	})

	w.check()
	expectNoEvent(t, received)
	if w.outageStart != (time.Time{}) {
		t.Error("should not set outageStart when one host succeeds")
	}
}

func TestConnectivityWatcher_NilHosts_DefaultsApplied(t *testing.T) {
	cfg := &config.Config{
		Connectivity: config.ConnectivityConfig{
			Enabled:      true,
			PollInterval: "30s",
			Hosts:        nil, // empty — constructor should default
		},
	}
	w := NewConnectivityWatcher(cfg, eventbus.New())
	if len(w.hosts) == 0 {
		t.Error("expected default hosts when config hosts is nil")
	}
	want := []string{"8.8.8.8:53", "1.1.1.1:53"}
	if len(w.hosts) != len(want) {
		t.Errorf("hosts = %v, want %v", w.hosts, want)
	}
	for i, h := range want {
		if w.hosts[i] != h {
			t.Errorf("hosts[%d] = %q, want %q", i, w.hosts[i], h)
		}
	}
}
