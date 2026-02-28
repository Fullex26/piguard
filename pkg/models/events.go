package models

import "time"

// Severity levels for events
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityCritical:
		return "critical"
	}
	return "unknown"
}

func (s Severity) Emoji() string {
	switch s {
	case SeverityInfo:
		return "ℹ️"
	case SeverityCritical:
		return "🔴"
	default:
		return "🟡"
	}
}

// EventType categorises what happened
type EventType string

const (
	EventPortOpened      EventType = "port.opened"
	EventPortClosed      EventType = "port.closed"
	EventFirewallChanged EventType = "firewall.changed"
	EventFirewallOK      EventType = "firewall.ok"
	EventSSHBruteForce   EventType = "ssh.bruteforce"
	EventDiskHigh        EventType = "system.disk_high"
	EventMemoryHigh      EventType = "system.memory_high"
	EventTempHigh        EventType = "system.temp_high"
	EventReboot          EventType = "system.reboot"
	EventContainerDied    EventType = "docker.container_died"
	EventContainerStart   EventType = "docker.container_start"
	EventContainerHealth  EventType = "docker.container_unhealthy"
	EventContainerStopped EventType = "docker.container_stopped"
	EventFileChanged     EventType = "file.changed"
	EventDailySummary    EventType = "summary.daily"
	EventMalwareFound    EventType = "malware.found"   // ClamAV FOUND line
	EventRootkitWarning  EventType = "rootkit.warning" // rkhunter Warning: line
)

// PortInfo describes a listening port with full context
type PortInfo struct {
	Address       string `json:"address"`        // e.g. "0.0.0.0:8080"
	Protocol      string `json:"protocol"`       // "tcp" or "udp"
	PID           int    `json:"pid"`
	ProcessName   string `json:"process_name"`   // e.g. "docker-proxy"
	ContainerName string `json:"container_name"` // e.g. "nginx" (empty if not Docker)
	ContainerID   string `json:"container_id"`
	IsExposed     bool   `json:"is_exposed"`     // true if bound to 0.0.0.0 or ::
}

func (p PortInfo) RiskLevel() Severity {
	if !p.IsExposed {
		return SeverityInfo // localhost only, low concern
	}
	return SeverityWarning // exposed to network
}

// FirewallState captures iptables chain state
type FirewallState struct {
	Chain        string `json:"chain"`
	Table        string `json:"table"`
	Policy       string `json:"policy"`
	RuleHash     string `json:"rule_hash"`
	HasDropRule  bool   `json:"has_drop_rule"`
}

// SystemHealth holds system metrics
type SystemHealth struct {
	DiskUsagePercent   int     `json:"disk_usage_percent"`
	MemoryUsedPercent  int     `json:"memory_used_percent"`
	CPUTempCelsius     float64 `json:"cpu_temp_celsius"`
	UptimeSeconds      int64   `json:"uptime_seconds"`
	ContainersRunning  int     `json:"containers_running"`
	ContainersHealthy  int     `json:"containers_healthy"`
	ListeningPorts     int     `json:"listening_ports"`
}

// Event is the core event structure that flows through the system
type Event struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`
	Severity  Severity  `json:"severity"`
	Hostname  string    `json:"hostname"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Details   string    `json:"details"`    // Extended info
	Suggested string    `json:"suggested"`  // Suggested fix action
	Source    string    `json:"source"`     // Which watcher generated this

	// Optional typed payloads
	Port     *PortInfo      `json:"port,omitempty"`
	Firewall *FirewallState `json:"firewall,omitempty"`
	Health   *SystemHealth  `json:"health,omitempty"`
}
