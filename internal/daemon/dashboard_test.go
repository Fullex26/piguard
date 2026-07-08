package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/store"
	"github.com/Fullex26/piguard/pkg/models"
)

func newTestDashboardStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "dashboard.db"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	events := []models.Event{
		{
			ID:        "network-1",
			Type:      models.EventNetworkNewDevice,
			Severity:  models.SeverityWarning,
			Hostname:  "pi",
			Timestamp: time.Now(),
			Message:   "New device on network: 192.168.1.55 (aa:bb:cc:dd:ee:ff)",
			Source:    "network-scan",
		},
		{
			ID:        "port-1",
			Type:      models.EventPortOpened,
			Severity:  models.SeverityWarning,
			Hostname:  "pi",
			Timestamp: time.Now(),
			Message:   "New listening port: 0.0.0.0:3001 -> docker-proxy",
			Source:    "netlink",
			Port:      &models.PortInfo{Address: "0.0.0.0:3001", IsExposed: true},
		},
		{
			ID:        "outage-1",
			Type:      models.EventConnectivityLost,
			Severity:  models.SeverityCritical,
			Hostname:  "pi",
			Timestamp: time.Now(),
			Message:   "Internet connectivity lost",
			Source:    "connectivity",
		},
	}
	for _, event := range events {
		if err := db.SaveEvent(event); err != nil {
			t.Fatalf("saving event: %v", err)
		}
	}

	return db
}

func TestDashboardSummaryAPI(t *testing.T) {
	db := newTestDashboardStore(t)
	mux := http.NewServeMux()
	registerDashboardHandlers(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var summary dashboardSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decoding summary: %v", err)
	}
	if summary.Events24h != 3 {
		t.Fatalf("Events24h = %d, want 3", summary.Events24h)
	}
	if summary.NewDevices != 1 {
		t.Errorf("NewDevices = %d, want 1", summary.NewDevices)
	}
	if summary.ExposedPorts != 1 {
		t.Errorf("ExposedPorts = %d, want 1", summary.ExposedPorts)
	}
	if summary.HighSeverity != 3 {
		t.Errorf("HighSeverity = %d, want 3", summary.HighSeverity)
	}
	if summary.RecentOutages != 1 {
		t.Errorf("RecentOutages = %d, want 1", summary.RecentOutages)
	}
}

func TestDashboardEventsAPILimit(t *testing.T) {
	db := newTestDashboardStore(t)
	mux := http.NewServeMux()
	registerDashboardHandlers(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/events?limit=1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var events []models.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decoding events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
}

func TestDashboardEventsAPIFilters(t *testing.T) {
	db := newTestDashboardStore(t)
	mux := http.NewServeMux()
	registerDashboardHandlers(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/events?type=port.opened&exposed=true", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var events []models.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decoding events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != models.EventPortOpened {
		t.Fatalf("event type = %s, want %s", events[0].Type, models.EventPortOpened)
	}
}

func TestDashboardHTML(t *testing.T) {
	db := newTestDashboardStore(t)
	mux := http.NewServeMux()
	registerDashboardHandlers(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "PiGuard") {
		t.Error("dashboard HTML missing title")
	}
	if !strings.Contains(body, "network.new_device") {
		t.Error("dashboard HTML missing event type")
	}
	for _, heading := range []string{"Unknown Devices", "Exposed Ports", "Warnings and Critical Events"} {
		if !strings.Contains(body, heading) {
			t.Errorf("dashboard HTML missing %q section", heading)
		}
	}
}
