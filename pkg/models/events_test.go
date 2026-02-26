package models

import "testing"

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
