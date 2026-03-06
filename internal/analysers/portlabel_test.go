package analysers

import (
	"testing"

	"github.com/Fullex26/piguard/pkg/models"
)

func TestPortLabeller_Label_ZeroPID(t *testing.T) {
	l := NewPortLabeller()
	port := models.PortInfo{PID: 0, Address: "0.0.0.0:80"}
	result := l.Label(port)
	if result.ProcessName != "" {
		t.Errorf("expected empty process name for PID 0, got %q", result.ProcessName)
	}
}

func TestPortLabeller_Label_ProcessName(t *testing.T) {
	l := NewPortLabeller()
	l.readProcessName = func(pid int) string {
		if pid == 1234 {
			return "nginx"
		}
		return "unknown"
	}

	port := models.PortInfo{PID: 1234, Address: "0.0.0.0:80"}
	result := l.Label(port)
	if result.ProcessName != "nginx" {
		t.Errorf("ProcessName = %q, want %q", result.ProcessName, "nginx")
	}
}

func TestPortLabeller_Label_PreexistingProcessName(t *testing.T) {
	l := NewPortLabeller()
	l.readProcessName = func(pid int) string {
		t.Fatal("readProcessName should not be called when ProcessName already set")
		return ""
	}

	port := models.PortInfo{PID: 1234, Address: "0.0.0.0:80", ProcessName: "already-set"}
	result := l.Label(port)
	if result.ProcessName != "already-set" {
		t.Errorf("ProcessName = %q, want %q", result.ProcessName, "already-set")
	}
}

func TestPortLabeller_Label_DockerProxy(t *testing.T) {
	l := NewPortLabeller()
	l.readProcessName = func(pid int) string { return "docker-proxy" }
	l.resolveContainer = func(addr string) containerInfo {
		return containerInfo{Name: "my-nginx", ID: "abc123"}
	}

	port := models.PortInfo{PID: 999, Address: "0.0.0.0:8080"}
	result := l.Label(port)
	if result.ContainerName != "my-nginx" {
		t.Errorf("ContainerName = %q, want %q", result.ContainerName, "my-nginx")
	}
	if result.ContainerID != "abc123" {
		t.Errorf("ContainerID = %q, want %q", result.ContainerID, "abc123")
	}
}

func TestPortLabeller_Label_NonDockerProcess(t *testing.T) {
	l := NewPortLabeller()
	l.readProcessName = func(pid int) string { return "node" }
	l.resolveContainer = func(addr string) containerInfo {
		t.Fatal("resolveContainer should not be called for non-docker-proxy process")
		return containerInfo{}
	}

	port := models.PortInfo{PID: 5678, Address: "127.0.0.1:3000"}
	result := l.Label(port)
	if result.ProcessName != "node" {
		t.Errorf("ProcessName = %q, want %q", result.ProcessName, "node")
	}
	if result.ContainerName != "" {
		t.Errorf("ContainerName = %q, want empty", result.ContainerName)
	}
}

func TestPortLabeller_Label_DockerProxy_NoContainer(t *testing.T) {
	l := NewPortLabeller()
	l.readProcessName = func(pid int) string { return "docker-proxy" }
	l.resolveContainer = func(addr string) containerInfo {
		return containerInfo{} // not found
	}

	port := models.PortInfo{PID: 999, Address: "0.0.0.0:9999"}
	result := l.Label(port)
	if result.ContainerName != "" {
		t.Errorf("ContainerName = %q, want empty", result.ContainerName)
	}
}

func TestPortLabeller_Label_ReadProcessName_UnknownPID(t *testing.T) {
	l := NewPortLabeller()
	l.readProcessName = func(pid int) string { return "unknown" }

	port := models.PortInfo{PID: 99999, Address: "0.0.0.0:80"}
	result := l.Label(port)
	if result.ProcessName != "unknown" {
		t.Errorf("ProcessName = %q, want %q", result.ProcessName, "unknown")
	}
}
