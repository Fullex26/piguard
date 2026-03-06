package daemon

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/analysers"
	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/notifiers"
	"github.com/Fullex26/piguard/internal/store"
	"github.com/Fullex26/piguard/pkg/models"
)

// mockNotifier records sent events for assertions.
type mockNotifier struct {
	mu     sync.Mutex
	events []models.Event
	raw    []string
}

func (m *mockNotifier) Name() string { return "mock" }

func (m *mockNotifier) Send(event models.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockNotifier) SendRaw(message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.raw = append(m.raw, message)
	return nil
}

func (m *mockNotifier) Test() error { return nil }

func (m *mockNotifier) SentEvents() []models.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]models.Event, len(m.events))
	copy(cp, m.events)
	return cp
}

// newTestDaemonWithStore builds a Daemon with temp SQLite, mock notifier, and given config.
func newTestDaemonWithStore(t *testing.T, cfg *config.Config) (*Daemon, *mockNotifier) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	bus := eventbus.New()
	mock := &mockNotifier{}

	d := &Daemon{
		cfg:       cfg,
		bus:       bus,
		store:     db,
		dedup:     analysers.NewDeduplicator(15 * time.Minute),
		notifiers: []notifiers.Notifier{mock},
	}

	bus.Subscribe(func(event models.Event) {
		d.handleEvent(event)
	})

	return d, mock
}

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		input   string
		wantH   int
		wantM   int
		wantErr bool
	}{
		{"23:00", 23, 0, false},
		{"07:00", 7, 0, false},
		{"00:00", 0, 0, false},
		{"12:30", 12, 30, false},
		{"", 0, 0, true},
		{"25:00", 0, 0, true},
		{"12:60", 0, 0, true},
		{"abc", 0, 0, true},
		{"12:ab", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			h, m, err := parseHHMM(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseHHMM(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && (h != tt.wantH || m != tt.wantM) {
				t.Errorf("parseHHMM(%q) = (%d, %d), want (%d, %d)", tt.input, h, m, tt.wantH, tt.wantM)
			}
		})
	}
}

func newTestDaemon(start, end string) *Daemon {
	return &Daemon{
		cfg: &config.Config{
			Alerts: config.AlertConfig{
				QuietHours: config.QuietHours{Start: start, End: end},
			},
		},
	}
}

func TestIsQuietHour_Overnight(t *testing.T) {
	d := newTestDaemon("23:00", "07:00")

	tests := []struct {
		name string
		time time.Time
		want bool
	}{
		{"23:30 is quiet", time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC), true},
		{"00:00 is quiet", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"06:59 is quiet", time.Date(2026, 1, 1, 6, 59, 0, 0, time.UTC), true},
		{"07:00 is not quiet", time.Date(2026, 1, 1, 7, 0, 0, 0, time.UTC), false},
		{"12:00 is not quiet", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), false},
		{"22:59 is not quiet", time.Date(2026, 1, 1, 22, 59, 0, 0, time.UTC), false},
		{"23:00 is quiet", time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.isQuietHour(tt.time); got != tt.want {
				t.Errorf("isQuietHour(%s) = %v, want %v", tt.time.Format("15:04"), got, tt.want)
			}
		})
	}
}

func TestIsQuietHour_SameDay(t *testing.T) {
	d := newTestDaemon("09:00", "17:00")

	tests := []struct {
		name string
		time time.Time
		want bool
	}{
		{"12:00 is quiet", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), true},
		{"09:00 is quiet", time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), true},
		{"08:00 is not quiet", time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC), false},
		{"17:00 is not quiet", time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC), false},
		{"23:00 is not quiet", time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.isQuietHour(tt.time); got != tt.want {
				t.Errorf("isQuietHour(%s) = %v, want %v", tt.time.Format("15:04"), got, tt.want)
			}
		})
	}
}

func TestIsQuietHour_Empty(t *testing.T) {
	d := newTestDaemon("", "")
	if d.isQuietHour(time.Now()) {
		t.Error("empty quiet hours should return false")
	}
}

func TestIsQuietHour_Invalid(t *testing.T) {
	d := newTestDaemon("abc", "def")
	if d.isQuietHour(time.Now()) {
		t.Error("invalid quiet hours should return false")
	}
}

func TestQuietHours_CriticalBypass(t *testing.T) {
	// This is a logic test: verify that the handleEvent flow would
	// NOT suppress Critical events during quiet hours.
	// We test the condition directly since handleEvent needs full wiring.
	d := newTestDaemon("00:00", "23:59")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// During quiet hours
	if !d.isQuietHour(now) {
		t.Fatal("expected quiet hour")
	}

	// Critical events bypass: the guard in handleEvent is:
	//   if d.isQuietHour(now) && event.Severity != models.SeverityCritical
	// So Critical should NOT be suppressed
	event := models.Event{Severity: models.SeverityCritical}
	suppressed := d.isQuietHour(now) && event.Severity != models.SeverityCritical
	if suppressed {
		t.Error("critical events should not be suppressed during quiet hours")
	}

	// Warning should be suppressed
	event.Severity = models.SeverityWarning
	suppressed = d.isQuietHour(now) && event.Severity != models.SeverityCritical
	if !suppressed {
		t.Error("warning events should be suppressed during quiet hours")
	}
}

// ── handleEvent tests ─────────────────────────────────────────────────────────

func testCfg() *config.Config {
	cfg := config.DefaultConfig()
	// Enable telegram so Validate() passes
	cfg.Notifications.Telegram.Enabled = true
	cfg.Notifications.Telegram.BotToken = "test"
	cfg.Notifications.Telegram.ChatID = "123"
	cfg.Alerts.MinSeverity = "info"
	cfg.Alerts.QuietHours = config.QuietHours{} // disable quiet hours
	return cfg
}

func TestHandleEvent_SavesAndNotifies(t *testing.T) {
	cfg := testCfg()
	d, mock := newTestDaemonWithStore(t, cfg)

	event := models.Event{
		ID:        "test-1",
		Type:      models.EventPortOpened,
		Severity:  models.SeverityWarning,
		Hostname:  "test",
		Timestamp: time.Now(),
		Message:   "port opened",
	}

	d.handleEvent(event)

	sent := mock.SentEvents()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent event, got %d", len(sent))
	}
	if sent[0].ID != "test-1" {
		t.Errorf("sent event ID = %q, want %q", sent[0].ID, "test-1")
	}
}

func TestHandleEvent_DedupSuppresses(t *testing.T) {
	cfg := testCfg()
	d, mock := newTestDaemonWithStore(t, cfg)

	event := models.Event{
		ID:        "test-dedup",
		Type:      models.EventDiskHigh,
		Severity:  models.SeverityWarning,
		Hostname:  "test",
		Timestamp: time.Now(),
		Message:   "disk high",
	}

	d.handleEvent(event) // first — goes through
	d.handleEvent(event) // second — deduped

	sent := mock.SentEvents()
	if len(sent) != 1 {
		t.Errorf("expected 1 sent event (second deduped), got %d", len(sent))
	}
}

func TestHandleEvent_QuietHoursSuppressWarning(t *testing.T) {
	cfg := testCfg()
	// Set quiet hours to cover all day
	cfg.Alerts.QuietHours = config.QuietHours{Start: "00:00", End: "23:59"}
	d, mock := newTestDaemonWithStore(t, cfg)

	warning := models.Event{
		ID:        "quiet-warn",
		Type:      models.EventMemoryHigh,
		Severity:  models.SeverityWarning,
		Hostname:  "test",
		Timestamp: time.Now(),
		Message:   "memory high",
	}

	d.handleEvent(warning)

	sent := mock.SentEvents()
	if len(sent) != 0 {
		t.Errorf("expected 0 sent events during quiet hours, got %d", len(sent))
	}
}

func TestHandleEvent_QuietHoursPassCritical(t *testing.T) {
	cfg := testCfg()
	cfg.Alerts.QuietHours = config.QuietHours{Start: "00:00", End: "23:59"}
	d, mock := newTestDaemonWithStore(t, cfg)

	critical := models.Event{
		ID:        "quiet-crit",
		Type:      models.EventFirewallChanged,
		Severity:  models.SeverityCritical,
		Hostname:  "test",
		Timestamp: time.Now(),
		Message:   "firewall changed",
	}

	d.handleEvent(critical)

	sent := mock.SentEvents()
	if len(sent) != 1 {
		t.Errorf("expected 1 sent event (critical bypasses quiet hours), got %d", len(sent))
	}
}

func TestHandleEvent_DifferentEventTypes(t *testing.T) {
	cfg := testCfg()
	d, mock := newTestDaemonWithStore(t, cfg)

	e1 := models.Event{
		ID: "ev-1", Type: models.EventPortOpened, Severity: models.SeverityWarning,
		Hostname: "test", Timestamp: time.Now(), Message: "port opened",
	}
	e2 := models.Event{
		ID: "ev-2", Type: models.EventDiskHigh, Severity: models.SeverityWarning,
		Hostname: "test", Timestamp: time.Now(), Message: "disk high",
	}

	d.handleEvent(e1)
	d.handleEvent(e2)

	sent := mock.SentEvents()
	if len(sent) != 2 {
		t.Errorf("expected 2 sent events (different types), got %d", len(sent))
	}
}

func TestParseWeekdayName(t *testing.T) {
	tests := []struct {
		input string
		want  time.Weekday
	}{
		{"sunday", time.Sunday},
		{"monday", time.Monday},
		{"tue", time.Tuesday},
		{"FRIDAY", time.Friday},
		{"unknown", time.Sunday},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseWeekdayName(tt.input); got != tt.want {
				t.Errorf("parseWeekdayName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
