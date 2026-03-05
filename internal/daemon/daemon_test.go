package daemon

import (
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/pkg/models"
)

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
