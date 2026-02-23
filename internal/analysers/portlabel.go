package analysers

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Fullex26/piguard/pkg/models"
)

// PortLabeller resolves port ownership: PID → process → container
type PortLabeller struct {
	containerCache map[int]containerInfo // PID -> container info
}

type containerInfo struct {
	Name string
	ID   string
}

func NewPortLabeller() *PortLabeller {
	return &PortLabeller{
		containerCache: make(map[int]containerInfo),
	}
}

// Label enriches a PortInfo with process and container details
func (l *PortLabeller) Label(port models.PortInfo) models.PortInfo {
	if port.PID == 0 {
		return port
	}

	// Resolve process name from /proc if not already set
	if port.ProcessName == "" {
		port.ProcessName = l.getProcessName(port.PID)
	}

	// If this is docker-proxy, resolve the actual container
	if port.ProcessName == "docker-proxy" {
		ci := l.resolveDockerContainer(port.Address)
		port.ContainerName = ci.Name
		port.ContainerID = ci.ID
	}

	return port
}

func (l *PortLabeller) getProcessName(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// resolveDockerContainer finds which container owns a published port
func (l *PortLabeller) resolveDockerContainer(addr string) containerInfo {
	// Parse port from address
	parts := strings.Split(addr, ":")
	if len(parts) < 2 {
		return containerInfo{}
	}
	port := parts[len(parts)-1]

	// Use docker inspect to find the container with this published port
	out, err := exec.Command("docker", "ps", "--format", "json").Output()
	if err != nil {
		return containerInfo{}
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var container struct {
			ID    string `json:"ID"`
			Names string `json:"Names"`
			Ports string `json:"Ports"`
		}
		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}
		// Check if this container exposes the port we're looking for
		if strings.Contains(container.Ports, ":"+port+"->") {
			return containerInfo{
				Name: container.Names,
				ID:   container.ID,
			}
		}
	}

	return containerInfo{}
}
