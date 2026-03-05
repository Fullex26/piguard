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

func newTestAutoUpdateWatcher() (*AutoUpdateWatcher, chan models.Event) {
	cfg := &config.Config{
		AutoUpdate: config.AutoUpdateConfig{Enabled: true, DayOfWeek: "daily", Time: "03:00"},
	}
	bus := eventbus.New()
	received := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { received <- e })
	w := NewAutoUpdateWatcher(cfg, bus)
	return w, received
}

func awaitAutoUpdateEvent(t *testing.T, ch chan models.Event) models.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return models.Event{}
	}
}

func expectNoAutoUpdateEvent(t *testing.T, ch chan models.Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("unexpected event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAutoUpdateWatcher_Name(t *testing.T) {
	w := &AutoUpdateWatcher{}
	if w.Name() != "auto-update" {
		t.Fatalf("expected auto-update, got %s", w.Name())
	}
}

func TestParseUpgradeCount(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect int
	}{
		{"three upgraded", "3 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.", 3},
		{"zero upgraded", "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.", 0},
		{"twelve upgraded", "12 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.", 12},
		{"empty output", "", 0},
		{"no match", "All packages are up to date.", 0},
		{"multiline", "Reading package lists...\n3 upgraded, 0 newly installed, 0 to remove.", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUpgradeCount(tt.input)
			if got != tt.expect {
				t.Errorf("parseUpgradeCount(%q) = %d, want %d", tt.input, got, tt.expect)
			}
		})
	}
}

func TestAutoUpdateWatcher_ScheduleMatch(t *testing.T) {
	tests := []struct {
		name   string
		day    string
		hhmm   string
		now    time.Time
		expect bool
	}{
		{
			"sunday 03:00 — match",
			"sunday", "03:00",
			time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC),
			true,
		},
		{
			"sunday 03:00 — wrong day",
			"sunday", "03:00",
			time.Date(2026, 3, 9, 3, 0, 0, 0, time.UTC),
			false,
		},
		{
			"sunday 03:00 — wrong time",
			"sunday", "03:00",
			time.Date(2026, 3, 8, 4, 0, 0, 0, time.UTC),
			false,
		},
		{
			"daily 03:00 — match on monday",
			"daily", "03:00",
			time.Date(2026, 3, 9, 3, 0, 0, 0, time.UTC),
			true,
		},
		{
			"daily 03:00 — match on friday",
			"daily", "03:00",
			time.Date(2026, 3, 13, 3, 0, 0, 0, time.UTC),
			true,
		},
		{
			"daily 03:00 — wrong time",
			"daily", "03:00",
			time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				AutoUpdate: config.AutoUpdateConfig{
					Enabled:   true,
					DayOfWeek: tt.day,
					Time:      tt.hhmm,
				},
			}
			bus := eventbus.New()
			w := NewAutoUpdateWatcher(cfg, bus)
			got := w.isScheduleMatch(tt.now)
			if got != tt.expect {
				t.Errorf("isScheduleMatch() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestAutoUpdateWatcher_RunUpgrade_Success(t *testing.T) {
	w, ch := newTestAutoUpdateWatcher()

	w.runApt = func(args ...string) (string, error) {
		if args[0] == "update" {
			return "Hit:1 http://...\n", nil
		}
		return "3 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.", nil
	}
	w.fileExist = func(path string) bool { return false }

	w.runUpgrade()
	e := awaitAutoUpdateEvent(t, ch)

	if e.Type != models.EventSystemUpdated {
		t.Errorf("expected EventSystemUpdated, got %s", e.Type)
	}
	if e.Severity != models.SeverityInfo {
		t.Errorf("expected SeverityInfo, got %s", e.Severity)
	}
	if e.Message != "System updated: 3 package(s) upgraded" {
		t.Errorf("unexpected message: %s", e.Message)
	}
}

func TestAutoUpdateWatcher_RunUpgrade_Failure(t *testing.T) {
	w, ch := newTestAutoUpdateWatcher()

	w.runApt = func(args ...string) (string, error) {
		return "E: Unable to lock the administration directory", fmt.Errorf("exit status 100")
	}
	w.fileExist = func(path string) bool { return false }

	w.runUpgrade()
	e := awaitAutoUpdateEvent(t, ch)

	if e.Type != models.EventSystemUpdateFailed {
		t.Errorf("expected EventSystemUpdateFailed, got %s", e.Type)
	}
	if e.Severity != models.SeverityWarning {
		t.Errorf("expected SeverityWarning, got %s", e.Severity)
	}
}

func TestAutoUpdateWatcher_RunUpgrade_RebootRequired(t *testing.T) {
	w, ch := newTestAutoUpdateWatcher()

	w.runApt = func(args ...string) (string, error) {
		if args[0] == "update" {
			return "Hit:1 http://...\n", nil
		}
		return "1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.", nil
	}
	w.fileExist = func(path string) bool {
		return path == "/var/run/reboot-required"
	}

	w.runUpgrade()
	e := awaitAutoUpdateEvent(t, ch)

	if e.Severity != models.SeverityWarning {
		t.Errorf("expected SeverityWarning for reboot-required, got %s", e.Severity)
	}
	if e.Suggested == "" {
		t.Error("expected a suggested action for reboot-required")
	}
	if e.Message != "System updated: 1 package(s) upgraded (reboot required)" {
		t.Errorf("unexpected message: %s", e.Message)
	}
}

func TestAutoUpdateWatcher_RunUpgrade_AlreadyUpToDate(t *testing.T) {
	w, ch := newTestAutoUpdateWatcher()

	w.runApt = func(args ...string) (string, error) {
		if args[0] == "update" {
			return "Hit:1 http://...\n", nil
		}
		return "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.", nil
	}
	w.fileExist = func(path string) bool { return false }

	w.runUpgrade()
	e := awaitAutoUpdateEvent(t, ch)

	if e.Message != "System update: already up to date" {
		t.Errorf("unexpected message: %s", e.Message)
	}
}

func TestAutoUpdateWatcher_CheckAvailable(t *testing.T) {
	w, _ := newTestAutoUpdateWatcher()

	w.runApt = func(args ...string) (string, error) {
		if args[0] == "list" {
			return "Listing...\ncurl/stable 7.88.1-10+deb12u5 arm64 [upgradable from: 7.88.1-10+deb12u4]\n", nil
		}
		return "", nil
	}

	out, err := w.CheckAvailable()
	if err != nil {
		t.Fatalf("CheckAvailable() error: %v", err)
	}
	if !strings.Contains(out, "curl") {
		t.Errorf("expected curl in output, got: %s", out)
	}
}

func TestParseWeekday(t *testing.T) {
	tests := map[string]time.Weekday{
		"sunday":    time.Sunday,
		"monday":    time.Monday,
		"tue":       time.Tuesday,
		"wednesday": time.Wednesday,
		"thu":       time.Thursday,
		"friday":    time.Friday,
		"sat":       time.Saturday,
		"unknown":   time.Sunday,
	}
	for input, expected := range tests {
		if got := parseWeekday(input); got != expected {
			t.Errorf("parseWeekday(%q) = %v, want %v", input, got, expected)
		}
	}
}

func TestAutoUpdateWatcher_UpgradeFailAtUpgrade(t *testing.T) {
	w, ch := newTestAutoUpdateWatcher()

	callCount := 0
	w.runApt = func(args ...string) (string, error) {
		callCount++
		if args[0] == "update" {
			return "OK", nil
		}
		return "dpkg error", fmt.Errorf("exit status 1")
	}
	w.fileExist = func(path string) bool { return false }

	w.runUpgrade()
	e := awaitAutoUpdateEvent(t, ch)

	if callCount != 2 {
		t.Errorf("expected 2 apt calls (update + upgrade), got %d", callCount)
	}
	if e.Type != models.EventSystemUpdateFailed {
		t.Errorf("expected EventSystemUpdateFailed, got %s", e.Type)
	}
}
