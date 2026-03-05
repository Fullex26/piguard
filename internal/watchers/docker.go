package watchers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

type containerState struct {
	ID      string `json:"ID"`
	Names   string `json:"Names"`
	Image   string `json:"Image"`
	ImageID string `json:"ImageID"` // full image digest, e.g. "sha256:abc123..."
	State   string `json:"State"`   // "running", "exited", "paused", "restarting"
	Status  string `json:"Status"`  // "Up 2 hours (healthy)", "Exited (1) 3 min ago"
	Ports   string `json:"Ports"`   // "0.0.0.0:8080->80/tcp, :::443->443/tcp"
}

// DockerWatcher polls for container lifecycle events.
type DockerWatcher struct {
	Base
	interval    time.Duration
	baseline    map[string]containerState // container ID → last known state
	nameToImage map[string]string         // container name → ImageID from previous cycle
	runDockerPS func() ([]byte, error)    // injectable for tests
}

func NewDockerWatcher(cfg *config.Config, bus *eventbus.Bus) *DockerWatcher {
	interval, err := time.ParseDuration(cfg.Docker.PollInterval)
	if err != nil || interval <= 0 {
		interval = 10 * time.Second
	}
	w := &DockerWatcher{
		Base:        Base{Cfg: cfg, Bus: bus},
		interval:    interval,
		baseline:    make(map[string]containerState),
		nameToImage: make(map[string]string),
	}
	w.runDockerPS = func() ([]byte, error) {
		return exec.Command("docker", "ps", "--all", "--no-trunc",
			"--format", "{{json .}}").Output()
	}
	return w
}

func (w *DockerWatcher) Name() string { return "docker" }
func (w *DockerWatcher) Stop() error  { return nil }

func (w *DockerWatcher) Start(ctx context.Context) error {
	slog.Info("starting docker watcher", "interval", w.interval)

	// Build initial baseline silently (no alerts for pre-existing containers).
	if containers, err := w.fetchContainers(); err == nil {
		for _, c := range containers {
			w.baseline[c.ID] = c
			w.nameToImage[c.Names] = c.ImageID
		}
		slog.Info("docker baseline established", "count", len(w.baseline))
	} else {
		slog.Warn("docker not available at startup", "error", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *DockerWatcher) check() {
	containers, err := w.fetchContainers()
	if err != nil {
		slog.Debug("docker check skipped", "error", err)
		return
	}

	current := make(map[string]containerState, len(containers))
	for _, c := range containers {
		current[c.ID] = c
	}

	// Rebuild name→imageID for Watchtower detection on the NEXT cycle.
	newNameToImage := make(map[string]string, len(containers))
	for _, c := range containers {
		newNameToImage[c.Names] = c.ImageID
	}

	hostname, _ := os.Hostname()

	for id, c := range current {
		prev, known := w.baseline[id]
		if !known {
			// Brand-new container ID — alert if it started running.
			if c.State == "running" {
				// Watchtower replaces a container: same name reappears with a different image digest.
				if prevImage, seen := w.nameToImage[c.Names]; seen && prevImage != "" && prevImage != c.ImageID {
					w.emit(hostname, models.EventContainerUpdated, models.SeverityInfo,
						fmt.Sprintf("Container updated: %s (%s)", c.Names, c.Image),
						"", c)
				} else {
					w.emit(hostname, models.EventContainerStart, models.SeverityInfo,
						fmt.Sprintf("Container started: %s (%s)", c.Names, c.Image),
						"", c)
				}
			}
			continue
		}
		// State transitions from running
		if prev.State == "running" && c.State == "exited" {
			exitCode := parseExitCode(c.Status)
			if exitCode != 0 {
				w.emit(hostname, models.EventContainerDied, models.SeverityWarning,
					fmt.Sprintf("Container crashed: %s (exit %d)", c.Names, exitCode),
					"Check container logs: docker logs "+c.Names, c)
			} else if w.Cfg.Docker.AlertOnStop {
				w.emit(hostname, models.EventContainerStopped, models.SeverityInfo,
					fmt.Sprintf("Container stopped: %s", c.Names), "", c)
			}
		}
		// Health transitions to unhealthy
		if isUnhealthy(c.Status) && !isUnhealthy(prev.Status) {
			w.emit(hostname, models.EventContainerHealth, models.SeverityWarning,
				fmt.Sprintf("Container unhealthy: %s", c.Names),
				"Check container logs: docker logs "+c.Names, c)
		}
		// Container restarted (was exited, now running again)
		if prev.State == "exited" && c.State == "running" {
			w.emit(hostname, models.EventContainerStart, models.SeverityInfo,
				fmt.Sprintf("Container restarted: %s (%s)", c.Names, c.Image), "", c)
		}
	}

	w.baseline = current
	w.nameToImage = newNameToImage
}

func (w *DockerWatcher) emit(hostname string, evType models.EventType, sev models.Severity,
	msg, suggested string, c containerState) {
	shortID := c.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	w.Bus.Publish(models.Event{
		ID:        fmt.Sprintf("%s-%s-%d", string(evType), shortID, time.Now().UnixNano()),
		Type:      evType,
		Severity:  sev,
		Hostname:  hostname,
		Timestamp: time.Now(),
		Message:   msg,
		Details:   fmt.Sprintf("Image: %s | Status: %s", c.Image, c.Status),
		Suggested: suggested,
		Source:    "docker",
	})
}

func (w *DockerWatcher) fetchContainers() ([]containerState, error) {
	out, err := w.runDockerPS()
	if err != nil {
		return nil, err
	}
	return parseDockerOutput(string(out))
}

// parseDockerOutput parses `docker ps --format '{{json .}}'` output (one JSON object per line).
func parseDockerOutput(output string) ([]containerState, error) {
	var containers []containerState
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c containerState
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue // skip malformed lines
		}
		containers = append(containers, c)
	}
	return containers, nil
}

// parseExitCode extracts the exit code from Status strings like "Exited (1) 3 minutes ago".
func parseExitCode(status string) int {
	start := strings.Index(status, "(")
	end := strings.Index(status, ")")
	if start < 0 || end <= start {
		return 0
	}
	code, _ := strconv.Atoi(status[start+1 : end])
	return code
}

// isUnhealthy returns true when the Status string contains "(unhealthy)".
func isUnhealthy(status string) bool {
	return strings.Contains(status, "(unhealthy)")
}
