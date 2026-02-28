package watchers

import (
	"fmt"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// ── parseDockerOutput ────────────────────────────────────────────────────────

func TestParseDockerOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{
			name:    "empty output",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "single running container",
			input:   `{"ID":"abc123","Names":"nginx","Image":"nginx:latest","State":"running","Status":"Up 2 hours"}`,
			wantLen: 1,
		},
		{
			name: "multiple containers",
			input: `{"ID":"abc123","Names":"nginx","Image":"nginx:latest","State":"running","Status":"Up 2 hours"}
{"ID":"def456","Names":"redis","Image":"redis:7","State":"exited","Status":"Exited (0) 5 min ago"}`,
			wantLen: 2,
		},
		{
			name: "malformed line is skipped",
			input: `{"ID":"abc123","Names":"nginx","Image":"nginx:latest","State":"running","Status":"Up 2 hours"}
not valid json
{"ID":"def456","Names":"redis","Image":"redis:7","State":"exited","Status":"Exited (1) 2 min ago"}`,
			wantLen: 2,
		},
		{
			name:    "blank lines ignored",
			input:   "\n\n",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDockerOutput(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestParseDockerOutput_Fields(t *testing.T) {
	input := `{"ID":"abc123full","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 3 hours (healthy)"}`
	got, err := parseDockerOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 container, got %d", len(got))
	}
	c := got[0]
	if c.ID != "abc123full" {
		t.Errorf("ID = %q, want %q", c.ID, "abc123full")
	}
	if c.Names != "myapp" {
		t.Errorf("Names = %q, want %q", c.Names, "myapp")
	}
	if c.State != "running" {
		t.Errorf("State = %q, want %q", c.State, "running")
	}
}

// ── parseExitCode ────────────────────────────────────────────────────────────

func TestParseExitCode(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{"Exited (0) 3 minutes ago", 0},
		{"Exited (1) 3 minutes ago", 1},
		{"Exited (137) 1 hour ago", 137},
		{"Up 2 hours", 0},
		{"", 0},
		{"Exited () bad", 0},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := parseExitCode(tt.status); got != tt.want {
				t.Errorf("parseExitCode(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

// ── isUnhealthy ──────────────────────────────────────────────────────────────

func TestIsUnhealthy(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"Up 3 hours (healthy)", false},
		{"Up 3 hours (unhealthy)", true},
		{"Up 1 minute", false},
		{"Exited (0) 5 min ago", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := isUnhealthy(tt.status); got != tt.want {
				t.Errorf("isUnhealthy(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ── DockerWatcher.Name ───────────────────────────────────────────────────────

func TestDockerWatcher_Name(t *testing.T) {
	cfg := &config.Config{Docker: config.DockerConfig{PollInterval: "10s"}}
	w := NewDockerWatcher(cfg, eventbus.New())
	if got := w.Name(); got != "docker" {
		t.Errorf("Name() = %q, want %q", got, "docker")
	}
}

// ── DockerWatcher.check — helper ─────────────────────────────────────────────

func newTestDockerWatcher(alertOnStop bool, stub func() ([]byte, error)) (*DockerWatcher, chan models.Event) {
	bus := eventbus.New()
	received := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { received <- e })

	cfg := &config.Config{
		Docker: config.DockerConfig{
			PollInterval: "10s",
			AlertOnStop:  alertOnStop,
		},
	}
	w := NewDockerWatcher(cfg, bus)
	w.runDockerPS = stub
	return w, received
}

func awaitEvent(t *testing.T, ch chan models.Event) models.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return models.Event{}
	}
}

func expectNoEvent(t *testing.T, ch chan models.Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Errorf("unexpected event: type=%s msg=%s", e.Type, e.Message)
	case <-time.After(50 * time.Millisecond):
		// good — no event
	}
}

// seedBaseline pre-populates the watcher's baseline from a docker ps output string,
// mirroring what Start() does before the first ticker tick.
func seedBaseline(w *DockerWatcher, output string) {
	containers, err := parseDockerOutput(output)
	if err != nil {
		return
	}
	for _, c := range containers {
		w.baseline[c.ID] = c
	}
}

// ── crash detection ──────────────────────────────────────────────────────────

func TestDockerWatcher_Check_CrashDetected(t *testing.T) {
	runningJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 1 hour"}`
	crashedJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"exited","Status":"Exited (1) 1 min ago"}`

	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return []byte(crashedJSON), nil
	})
	seedBaseline(w, runningJSON)

	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventContainerDied {
		t.Errorf("event type = %q, want %q", e.Type, models.EventContainerDied)
	}
	if e.Severity != models.SeverityWarning {
		t.Errorf("severity = %v, want Warning", e.Severity)
	}
}

// ── graceful stop — AlertOnStop=false (default, no alert) ────────────────────

func TestDockerWatcher_Check_GracefulStop_NoAlert(t *testing.T) {
	runningJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 1 hour"}`
	stoppedJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"exited","Status":"Exited (0) 1 min ago"}`

	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return []byte(stoppedJSON), nil
	})
	seedBaseline(w, runningJSON)

	w.check()
	expectNoEvent(t, received)
}

// ── graceful stop — AlertOnStop=true ─────────────────────────────────────────

func TestDockerWatcher_Check_GracefulStop_WithAlert(t *testing.T) {
	runningJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 1 hour"}`
	stoppedJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"exited","Status":"Exited (0) 1 min ago"}`

	w, received := newTestDockerWatcher(true, func() ([]byte, error) {
		return []byte(stoppedJSON), nil
	})
	seedBaseline(w, runningJSON)

	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventContainerStopped {
		t.Errorf("event type = %q, want %q", e.Type, models.EventContainerStopped)
	}
}

// ── unhealthy transition ──────────────────────────────────────────────────────

func TestDockerWatcher_Check_UnhealthyTransition(t *testing.T) {
	healthyJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 1 hour (healthy)"}`
	unhealthyJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 1 hour (unhealthy)"}`

	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return []byte(unhealthyJSON), nil
	})
	seedBaseline(w, healthyJSON)

	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventContainerHealth {
		t.Errorf("event type = %q, want %q", e.Type, models.EventContainerHealth)
	}
	if e.Severity != models.SeverityWarning {
		t.Errorf("severity = %v, want Warning", e.Severity)
	}
}

// ── no alert when already unhealthy ──────────────────────────────────────────

func TestDockerWatcher_Check_AlreadyUnhealthy_NoRepeat(t *testing.T) {
	unhealthyJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 1 hour (unhealthy)"}`

	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return []byte(unhealthyJSON), nil
	})
	// Seed baseline already unhealthy — no transition, so no alert
	seedBaseline(w, unhealthyJSON)

	w.check()
	expectNoEvent(t, received)
}

// ── restart detection ─────────────────────────────────────────────────────────

func TestDockerWatcher_Check_ContainerRestart(t *testing.T) {
	exitedJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"exited","Status":"Exited (1) 2 min ago"}`
	runningJSON := `{"ID":"abc123fullid","Names":"myapp","Image":"myimage:v1","State":"running","Status":"Up 5 seconds"}`

	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return []byte(runningJSON), nil
	})
	seedBaseline(w, exitedJSON)

	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventContainerStart {
		t.Errorf("event type = %q, want %q", e.Type, models.EventContainerStart)
	}
}

// ── new container starts running ──────────────────────────────────────────────

func TestDockerWatcher_Check_NewContainerRunning(t *testing.T) {
	newContainerJSON := `{"ID":"newcontainer123","Names":"freshapp","Image":"freshapp:latest","State":"running","Status":"Up 1 second"}`

	// Empty baseline — new container appears running
	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return []byte(newContainerJSON), nil
	})

	w.check()

	e := awaitEvent(t, received)
	if e.Type != models.EventContainerStart {
		t.Errorf("event type = %q, want %q", e.Type, models.EventContainerStart)
	}
}

// ── docker unavailable — no panic ─────────────────────────────────────────────

func TestDockerWatcher_Check_DockerUnavailable(t *testing.T) {
	w, received := newTestDockerWatcher(false, func() ([]byte, error) {
		return nil, fmt.Errorf("docker not found")
	})

	// Should log debug and return without panicking
	w.check()
	expectNoEvent(t, received)
}
