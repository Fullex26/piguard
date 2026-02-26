package watchers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

func TestSecToolsWatcher_Name(t *testing.T) {
	w := &SecToolsWatcher{}
	if got := w.Name(); got != "sectools" {
		t.Errorf("Name() = %q, want %q", got, "sectools")
	}
}

func TestIsClamAVMatch(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"found", "/path/to/file: Win.Trojan.Agent FOUND", true},
		{"found but no such file", "LibClamAV Warning: /tmp/missing: No such file FOUND", false},
		{"ok line", "/path/to/file: OK", false},
		{"empty", "", false},
		{"just FOUND", "FOUND", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClamAVMatch(tt.line); got != tt.want {
				t.Errorf("isClamAVMatch(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestIsRKHunterMatch(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"warning", "Warning: Application 'rsh' found", true},
		{"timestamped warning", "[12:00] Warning: something suspicious", true},
		{"info line", "Info: no warnings", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRKHunterMatch(tt.line); got != tt.want {
				t.Errorf("isRKHunterMatch(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestSuggestedAction(t *testing.T) {
	tests := []struct {
		evType models.EventType
		substr string
	}{
		{models.EventMalwareFound, "clamscan"},
		{models.EventRootkitWarning, "rkhunter"},
		{models.EventPortOpened, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.evType), func(t *testing.T) {
			got := suggestedAction(tt.evType)
			if tt.substr == "" {
				if got != "" {
					t.Errorf("suggestedAction(%q) = %q, want empty", tt.evType, got)
				}
			} else if got == "" || !contains(got, tt.substr) {
				t.Errorf("suggestedAction(%q) = %q, want it to contain %q", tt.evType, got, tt.substr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSecToolsWatcher_ScanLog_NewLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Write initial content
	initial := "some old line\n"
	os.WriteFile(logPath, []byte(initial), 0600)

	bus := eventbus.New()
	received := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) {
		received <- e
	})

	cfg := &config.Config{
		SecurityTools: config.SecurityToolsConfig{
			ClamAVLog: logPath,
		},
	}
	w := &SecToolsWatcher{
		Base:    Base{Cfg: cfg, Bus: bus},
		offsets: map[string]int64{logPath: int64(len(initial))},
	}

	// Append a matching line
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("/tmp/evil: Win.Trojan FOUND\n")
	f.Close()

	w.scanLog(logPath, models.EventMalwareFound, isClamAVMatch)

	select {
	case e := <-received:
		if e.Type != models.EventMalwareFound {
			t.Errorf("event type = %q, want %q", e.Type, models.EventMalwareFound)
		}
	default:
		t.Error("expected an event to be published")
	}
}

func TestSecToolsWatcher_ScanLog_LogRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Small file (simulating rotation)
	os.WriteFile(logPath, []byte("short\n"), 0600)

	w := &SecToolsWatcher{
		Base:    Base{Cfg: &config.Config{}, Bus: eventbus.New()},
		offsets: map[string]int64{logPath: 1000}, // old offset > file size
	}

	// scanLog should reset offset
	w.scanLog(logPath, models.EventMalwareFound, isClamAVMatch)

	if w.offsets[logPath] >= 1000 {
		t.Errorf("offset should have been reset, got %d", w.offsets[logPath])
	}
}

func TestSecToolsWatcher_ScanLog_MissingFile(t *testing.T) {
	w := &SecToolsWatcher{
		Base:    Base{Cfg: &config.Config{}, Bus: eventbus.New()},
		offsets: make(map[string]int64),
	}

	// Should not panic or error
	w.scanLog("/nonexistent/path/log.txt", models.EventMalwareFound, isClamAVMatch)
}
