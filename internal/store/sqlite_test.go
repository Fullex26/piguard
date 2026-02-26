package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fullex26/piguard/pkg/models"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func makeEvent(id string, sev models.Severity, ts time.Time) models.Event {
	return models.Event{
		ID:        id,
		Type:      models.EventPortOpened,
		Severity:  sev,
		Hostname:  "test-host",
		Timestamp: ts,
		Message:   "test event " + id,
		Source:    "test",
	}
}

func TestOpen_CreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	s.Close()
}

func TestOpen_IdempotentMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	s2.Close()
}

func TestSaveEvent_AndGetRecentEvents(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	for i := range 3 {
		e := makeEvent("e"+string(rune('0'+i)), models.SeverityWarning, now.Add(-time.Duration(i)*time.Minute))
		if err := s.SaveEvent(e); err != nil {
			t.Fatalf("SaveEvent: %v", err)
		}
	}

	events, err := s.GetRecentEvents(1)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("got %d events, want 3", len(events))
	}

	// Verify descending order by timestamp
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.After(events[i-1].Timestamp) {
			t.Error("events not in descending timestamp order")
		}
	}
}

func TestGetRecentEvents_Limit(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	for i := range 110 {
		e := makeEvent("e"+string(rune(i)), models.SeverityInfo, now.Add(-time.Duration(i)*time.Second))
		if err := s.SaveEvent(e); err != nil {
			t.Fatalf("SaveEvent: %v", err)
		}
	}

	events, err := s.GetRecentEvents(24)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(events) > 100 {
		t.Errorf("got %d events, want at most 100", len(events))
	}
}

func TestGetRecentEvents_TimeFiltering(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	old := makeEvent("old", models.SeverityWarning, now.Add(-2*time.Hour))
	recent := makeEvent("recent", models.SeverityWarning, now.Add(-30*time.Minute))

	s.SaveEvent(old)
	s.SaveEvent(recent)

	events, err := s.GetRecentEvents(1)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].ID != "recent" {
		t.Errorf("got event ID %q, want %q", events[0].ID, "recent")
	}
}

func TestGetRecentEvents_Empty(t *testing.T) {
	s := openTestStore(t)
	events, err := s.GetRecentEvents(1)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if events != nil && len(events) != 0 {
		t.Errorf("expected empty slice, got %d events", len(events))
	}
}

func TestSaveEvent_PayloadRoundTrip(t *testing.T) {
	s := openTestStore(t)

	want := models.Event{
		ID:        "roundtrip",
		Type:      models.EventPortOpened,
		Severity:  models.SeverityWarning,
		Hostname:  "pi",
		Timestamp: time.Now().Truncate(time.Second),
		Message:   "test",
		Details:   "detail",
		Suggested: "fix it",
		Source:    "test",
		Port: &models.PortInfo{
			Address:   "0.0.0.0:8080",
			Protocol:  "tcp",
			PID:       1234,
			IsExposed: true,
		},
		Firewall: &models.FirewallState{
			Chain:  "INPUT",
			Policy: "DROP",
		},
		Health: &models.SystemHealth{
			DiskUsagePercent:  50,
			MemoryUsedPercent: 60,
		},
	}

	if err := s.SaveEvent(want); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}

	events, err := s.GetRecentEvents(24)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	got := events[0]
	if got.ID != want.ID || got.Message != want.Message || got.Details != want.Details {
		t.Errorf("basic fields mismatch")
	}
	if got.Port == nil || got.Port.Address != "0.0.0.0:8080" {
		t.Error("Port payload lost in round trip")
	}
	if got.Firewall == nil || got.Firewall.Chain != "INPUT" {
		t.Error("Firewall payload lost in round trip")
	}
	if got.Health == nil || got.Health.DiskUsagePercent != 50 {
		t.Error("Health payload lost in round trip")
	}
}

func TestGetLastAlertTime_NoEvents(t *testing.T) {
	s := openTestStore(t)
	result, err := s.GetLastAlertTime()
	if err != nil {
		t.Fatalf("GetLastAlertTime: %v", err)
	}
	if result != "never" {
		t.Errorf("got %q, want %q", result, "never")
	}
}

func TestGetLastAlertTime_RecentEvent(t *testing.T) {
	s := openTestStore(t)
	e := makeEvent("recent-alert", models.SeverityWarning, time.Now().Add(-5*time.Minute))
	s.SaveEvent(e)

	result, err := s.GetLastAlertTime()
	if err != nil {
		t.Fatalf("GetLastAlertTime: %v", err)
	}
	if result == "never" {
		t.Error("expected a time, got 'never'")
	}
}

func TestGetLastAlertTime_InfoOnlyEvents(t *testing.T) {
	s := openTestStore(t)
	e := makeEvent("info-only", models.SeverityInfo, time.Now())
	s.SaveEvent(e)

	result, err := s.GetLastAlertTime()
	if err != nil {
		t.Fatalf("GetLastAlertTime: %v", err)
	}
	if result != "never" {
		t.Errorf("info-only events should return 'never', got %q", result)
	}
}

func TestGetEventCount(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	// 3 recent events
	for i := range 3 {
		e := makeEvent("r"+string(rune('0'+i)), models.SeverityInfo, now.Add(-time.Duration(i)*time.Minute))
		s.SaveEvent(e)
	}
	// 2 old events
	for i := range 2 {
		e := makeEvent("o"+string(rune('0'+i)), models.SeverityInfo, now.Add(-25*time.Hour-time.Duration(i)*time.Minute))
		s.SaveEvent(e)
	}

	count, err := s.GetEventCount(24)
	if err != nil {
		t.Fatalf("GetEventCount: %v", err)
	}
	if count != 3 {
		t.Errorf("got count %d, want 3", count)
	}
}

func TestSetState_GetState(t *testing.T) {
	s := openTestStore(t)

	if err := s.SetState("key1", "value1"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, err := s.GetState("key1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got != "value1" {
		t.Errorf("got %q, want %q", got, "value1")
	}
}

func TestSetState_Overwrite(t *testing.T) {
	s := openTestStore(t)

	s.SetState("key1", "a")
	s.SetState("key1", "b")

	got, err := s.GetState("key1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got != "b" {
		t.Errorf("got %q, want %q", got, "b")
	}
}

func TestGetState_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetState("nonexistent")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestPrune(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	// 2 old events (60 days ago)
	for i := range 2 {
		e := makeEvent("old"+string(rune('0'+i)), models.SeverityInfo, now.AddDate(0, 0, -60))
		s.SaveEvent(e)
	}
	// 3 recent events
	for i := range 3 {
		e := makeEvent("new"+string(rune('0'+i)), models.SeverityInfo, now)
		s.SaveEvent(e)
	}

	affected, err := s.Prune(30)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected = %d, want 2", affected)
	}

	count, _ := s.GetEventCount(24 * 365)
	if count != 3 {
		t.Errorf("remaining events = %d, want 3", count)
	}
}

func TestPrune_NothingToDelete(t *testing.T) {
	s := openTestStore(t)
	e := makeEvent("recent", models.SeverityInfo, time.Now())
	s.SaveEvent(e)

	affected, err := s.Prune(30)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if affected != 0 {
		t.Errorf("affected = %d, want 0", affected)
	}
}
