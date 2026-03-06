package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityCritical, "critical"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestSeverity_Emoji(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{SeverityInfo, "ℹ️"},
		{SeverityCritical, "🔴"},
		{SeverityWarning, "🟡"},
		{Severity(99), "🟡"}, // default case
	}
	for _, tt := range tests {
		if got := tt.sev.Emoji(); got != tt.want {
			t.Errorf("Severity(%d).Emoji() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestPortInfo_RiskLevel(t *testing.T) {
	tests := []struct {
		name string
		port PortInfo
		want Severity
	}{
		{"not exposed", PortInfo{IsExposed: false}, SeverityInfo},
		{"exposed", PortInfo{IsExposed: true}, SeverityWarning},
		{"zero value", PortInfo{}, SeverityInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.port.RiskLevel(); got != tt.want {
				t.Errorf("RiskLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventType_Uniqueness(t *testing.T) {
	types := []EventType{
		EventPortOpened, EventPortClosed,
		EventFirewallChanged, EventFirewallOK,
		EventSSHBruteForce, EventSudoFailure, EventSSHLogin,
		EventDiskHigh, EventMemoryHigh, EventTempHigh, EventReboot,
		EventContainerDied, EventContainerStart, EventContainerHealth, EventContainerStopped,
		EventFileChanged, EventDailySummary, EventWeeklySummary,
		EventMalwareFound, EventRootkitWarning,
		EventNetworkNewDevice, EventNetworkDeviceLeft,
		EventConnectivityLost, EventConnectivityRestored,
		EventContainerUpdated, EventSystemUpdated, EventSystemUpdateFailed,
	}

	seen := make(map[EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Errorf("duplicate EventType constant: %q", et)
		}
		seen[et] = true
	}
}

func TestEvent_ZeroValue(t *testing.T) {
	var e Event
	if e.ID != "" {
		t.Errorf("zero-value ID = %q, want empty", e.ID)
	}
	if e.Severity != SeverityInfo {
		t.Errorf("zero-value Severity = %v, want SeverityInfo (0)", e.Severity)
	}
	if e.Port != nil {
		t.Error("zero-value Port should be nil")
	}
	if e.Firewall != nil {
		t.Error("zero-value Firewall should be nil")
	}
	if e.Health != nil {
		t.Error("zero-value Health should be nil")
	}
}

func TestEvent_JSONRoundTrip(t *testing.T) {
	original := Event{
		ID:        "test-json",
		Type:      EventPortOpened,
		Severity:  SeverityWarning,
		Hostname:  "pi",
		Timestamp: time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC),
		Message:   "port opened",
		Port: &PortInfo{
			Address:     "0.0.0.0:8080",
			Protocol:    "tcp",
			PID:         1234,
			ProcessName: "nginx",
			IsExposed:   true,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, original.Type)
	}
	if decoded.Port == nil {
		t.Fatal("Port is nil after round-trip")
	}
	if decoded.Port.Address != "0.0.0.0:8080" {
		t.Errorf("Port.Address = %q, want %q", decoded.Port.Address, "0.0.0.0:8080")
	}
}

func TestEvent_JSONRoundTrip_Firewall(t *testing.T) {
	original := Event{
		ID:       "fw-test",
		Type:     EventFirewallChanged,
		Severity: SeverityCritical,
		Firewall: &FirewallState{
			Chain:  "INPUT",
			Table:  "filter",
			Policy: "DROP",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Firewall == nil {
		t.Fatal("Firewall is nil after round-trip")
	}
	if decoded.Firewall.Chain != "INPUT" {
		t.Errorf("Firewall.Chain = %q, want %q", decoded.Firewall.Chain, "INPUT")
	}
}

func TestEvent_JSONRoundTrip_Health(t *testing.T) {
	original := Event{
		ID:   "health-test",
		Type: EventDiskHigh,
		Health: &SystemHealth{
			DiskUsagePercent:  85,
			MemoryUsedPercent: 60,
			CPUTempCelsius:    55.5,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Health == nil {
		t.Fatal("Health is nil after round-trip")
	}
	if decoded.Health.DiskUsagePercent != 85 {
		t.Errorf("Health.DiskUsagePercent = %d, want 85", decoded.Health.DiskUsagePercent)
	}
}

func TestEvent_JSONOmitsNilPayloads(t *testing.T) {
	e := Event{ID: "minimal", Type: EventDiskHigh}
	data, _ := json.Marshal(e)
	s := string(data)

	if contains(s, "port") {
		t.Errorf("JSON should omit nil port, got: %s", s)
	}
	if contains(s, "firewall") {
		t.Errorf("JSON should omit nil firewall, got: %s", s)
	}
	if contains(s, "health") {
		t.Errorf("JSON should omit nil health, got: %s", s)
	}
}

func TestSeverity_Ordering(t *testing.T) {
	if SeverityInfo >= SeverityWarning {
		t.Error("Info should be less than Warning")
	}
	if SeverityWarning >= SeverityCritical {
		t.Error("Warning should be less than Critical")
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
