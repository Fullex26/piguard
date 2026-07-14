package watchers

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// ── parseIPNeigh ─────────────────────────────────────────────────────────────

func TestParseIPNeigh_Empty(t *testing.T) {
	if got := parseIPNeigh(""); len(got) != 0 {
		t.Errorf("expected 0 devices, got %d", len(got))
	}
}

func TestParseIPNeigh_ReachableAndStale(t *testing.T) {
	input := `192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE
192.168.1.42 dev eth0 lladdr 11:22:33:44:55:66 STALE`
	got := parseIPNeigh(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	if got[0].IP != "192.168.1.1" || got[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("unexpected first device: %+v", got[0])
	}
	if got[1].IP != "192.168.1.42" || got[1].MAC != "11:22:33:44:55:66" {
		t.Errorf("unexpected second device: %+v", got[1])
	}
}

func TestParseIPNeigh_SkipsFailed(t *testing.T) {
	input := `192.168.1.99 dev eth0 FAILED
192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE`
	got := parseIPNeigh(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 device (FAILED skipped), got %d", len(got))
	}
}

func TestParseIPNeigh_SkipsIncomplete(t *testing.T) {
	input := `192.168.1.200 dev eth0 INCOMPLETE
192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE`
	got := parseIPNeigh(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 device (INCOMPLETE skipped), got %d", len(got))
	}
}

func TestParseIPNeigh_NormalisesMAC(t *testing.T) {
	input := `192.168.1.1 dev eth0 lladdr AA:BB:CC:DD:EE:FF REACHABLE`
	got := parseIPNeigh(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 device, got %d", len(got))
	}
	if got[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC not normalised: %q", got[0].MAC)
	}
}

func TestParseIPNeigh_NoLLAddr(t *testing.T) {
	// Lines without lladdr (e.g. directly-attached interface) should be skipped.
	input := `192.168.1.1 dev eth0 REACHABLE`
	got := parseIPNeigh(input)
	if len(got) != 0 {
		t.Errorf("expected 0 devices for no-lladdr line, got %d", len(got))
	}
}

func TestParseIPNeigh_BlankLines(t *testing.T) {
	input := "\n\n"
	got := parseIPNeigh(input)
	if len(got) != 0 {
		t.Errorf("expected 0 devices for blank input, got %d", len(got))
	}
}

func TestParseIPNeigh_SkipsDockerInterfaces(t *testing.T) {
	input := `172.17.0.2 dev docker0 lladdr aa:bb:cc:dd:ee:01 REACHABLE
172.18.0.2 dev br-1234 lladdr aa:bb:cc:dd:ee:02 STALE
192.168.1.10 dev eth0 lladdr aa:bb:cc:dd:ee:03 REACHABLE`
	got := parseIPNeigh(input)
	if len(got) != 1 || got[0].IP != "192.168.1.10" {
		t.Fatalf("expected only the LAN device, got %+v", got)
	}
}

type memoryNetworkState struct{ value string }

func (s *memoryNetworkState) GetState(string) (string, error) { return s.value, nil }
func (s *memoryNetworkState) SetState(_ string, value string) error {
	s.value = value
	return nil
}

func TestNetworkScanWatcher_PersistsBaseline(t *testing.T) {
	state := &memoryNetworkState{}
	w, _ := newTestNetworkWatcher(false, nil, func() ([]byte, error) {
		return []byte(`192.168.1.50 dev eth0 lladdr de:ad:be:ef:00:01 REACHABLE`), nil
	})
	w.stateStore = state
	w.check()
	if !strings.Contains(state.value, "de:ad:be:ef:00:01") {
		t.Fatalf("saved baseline does not contain device: %s", state.value)
	}

	w2, received := newTestNetworkWatcher(false, nil, func() ([]byte, error) {
		return []byte(`192.168.1.50 dev eth0 lladdr de:ad:be:ef:00:01 REACHABLE`), nil
	})
	w2.stateStore = state
	w2.loadBaseline()
	w2.check()
	select {
	case event := <-received:
		t.Fatalf("persisted device generated a new-device event: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNetworkScanWatcher_StartupAlertsForDeviceMissingFromSavedBaseline(t *testing.T) {
	state := &memoryNetworkState{value: `{"aa:bb:cc:dd:ee:ff":{"ip":"192.168.1.1","mac":"aa:bb:cc:dd:ee:ff","interface":"eth0"}}`}
	w, received := newTestNetworkWatcher(false, nil, nil)
	w.stateStore = state
	if !w.loadBaseline() {
		t.Fatal("expected persisted baseline to load")
	}

	w.reconcileStartup(parseIPNeigh(`192.168.1.50 dev eth0 lladdr de:ad:be:ef:00:01 REACHABLE`), true)

	select {
	case event := <-received:
		if event.Type != models.EventNetworkNewDevice || !strings.Contains(event.Message, "de:ad:be:ef:00:01") {
			t.Fatalf("unexpected startup event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for startup new-device event")
	}
}

func TestNetworkScanWatcher_FirstStartupLearnsSilently(t *testing.T) {
	w, received := newTestNetworkWatcher(false, nil, nil)
	w.reconcileStartup(parseIPNeigh(`192.168.1.50 dev eth0 lladdr de:ad:be:ef:00:01 REACHABLE`), false)

	select {
	case event := <-received:
		t.Fatalf("first startup generated an event: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

// ── NetworkScanWatcher helper ─────────────────────────────────────────────────

func newTestNetworkWatcher(alertLeave bool, ignoreMACList []string, stub func() ([]byte, error)) (*NetworkScanWatcher, chan models.Event) {
	bus := eventbus.New()
	received := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { received <- e })

	cfg := &config.Config{
		Network: config.NetworkConfig{
			PollInterval: "5m",
			AlertOnLeave: alertLeave,
			IgnoreMACs:   ignoreMACList,
		},
	}
	w := NewNetworkScanWatcher(cfg, bus)
	w.runIPNeigh = stub
	return w, received
}

func seedNetworkBaseline(w *NetworkScanWatcher, output string) {
	for _, d := range parseIPNeigh(output) {
		w.baseline[d.MAC] = d
	}
}

// ── new device detection ──────────────────────────────────────────────────────

func TestNetworkScanWatcher_NewDevice(t *testing.T) {
	newDeviceOutput := `192.168.1.50 dev eth0 lladdr de:ad:be:ef:00:01 REACHABLE`

	w, received := newTestNetworkWatcher(false, nil, func() ([]byte, error) {
		return []byte(newDeviceOutput), nil
	})
	// Empty baseline — device is new.
	w.check()

	select {
	case e := <-received:
		if e.Type != models.EventNetworkNewDevice {
			t.Errorf("event type = %q, want %q", e.Type, models.EventNetworkNewDevice)
		}
		if e.Severity != models.SeverityWarning {
			t.Errorf("severity = %v, want Warning", e.Severity)
		}
		if e.Suggested == "" {
			t.Error("expected suggested action for new device event")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for new device event")
	}
}

// ── no alert for known device ─────────────────────────────────────────────────

func TestNetworkScanWatcher_KnownDevice_NoAlert(t *testing.T) {
	deviceOutput := `192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE`

	w, received := newTestNetworkWatcher(false, nil, func() ([]byte, error) {
		return []byte(deviceOutput), nil
	})
	seedNetworkBaseline(w, deviceOutput)

	w.check()

	select {
	case e := <-received:
		t.Errorf("unexpected event: type=%s msg=%s", e.Type, e.Message)
	case <-time.After(50 * time.Millisecond):
		// good — no event
	}
}

// ── device left — alertLeave=false (default) ──────────────────────────────────

func TestNetworkScanWatcher_DeviceLeft_NoAlert(t *testing.T) {
	knownDevice := `192.168.1.10 dev eth0 lladdr ff:ff:ff:ff:ff:01 REACHABLE`

	w, received := newTestNetworkWatcher(false, nil, func() ([]byte, error) {
		return []byte(""), nil // no devices visible now
	})
	seedNetworkBaseline(w, knownDevice)

	w.check()

	select {
	case e := <-received:
		t.Errorf("unexpected event (alertLeave=false): type=%s", e.Type)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

// ── device left — alertLeave=true ─────────────────────────────────────────────

func TestNetworkScanWatcher_DeviceLeft_WithAlert(t *testing.T) {
	knownDevice := `192.168.1.10 dev eth0 lladdr ff:ff:ff:ff:ff:01 REACHABLE`

	w, received := newTestNetworkWatcher(true, nil, func() ([]byte, error) {
		return []byte(""), nil // device gone
	})
	seedNetworkBaseline(w, knownDevice)

	w.check()

	select {
	case e := <-received:
		if e.Type != models.EventNetworkDeviceLeft {
			t.Errorf("event type = %q, want %q", e.Type, models.EventNetworkDeviceLeft)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for device-left event")
	}
}

// ── ignored MAC — no alert even if new ───────────────────────────────────────

func TestNetworkScanWatcher_IgnoredMAC_NoAlert(t *testing.T) {
	ignoredMAC := "aa:bb:cc:dd:ee:ff"
	deviceOutput := fmt.Sprintf("192.168.1.1 dev eth0 lladdr %s REACHABLE", ignoredMAC)

	w, received := newTestNetworkWatcher(false, []string{ignoredMAC}, func() ([]byte, error) {
		return []byte(deviceOutput), nil
	})
	// Empty baseline — would normally trigger new-device alert, but MAC is ignored.
	w.check()

	select {
	case e := <-received:
		t.Errorf("unexpected event for ignored MAC: type=%s", e.Type)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

// ── ip neigh unavailable — no panic ──────────────────────────────────────────

func TestNetworkScanWatcher_IPNeighUnavailable(t *testing.T) {
	w, received := newTestNetworkWatcher(false, nil, func() ([]byte, error) {
		return nil, fmt.Errorf("ip command not found")
	})

	w.check() // must not panic

	select {
	case e := <-received:
		t.Errorf("unexpected event: type=%s", e.Type)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

// ── Name ─────────────────────────────────────────────────────────────────────

func TestNetworkScanWatcher_Name(t *testing.T) {
	cfg := &config.Config{Network: config.NetworkConfig{PollInterval: "5m"}}
	w := NewNetworkScanWatcher(cfg, eventbus.New())
	if got := w.Name(); got != "network-scan" {
		t.Errorf("Name() = %q, want %q", got, "network-scan")
	}
}
