package notifiers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fullex26/piguard/pkg/models"
)

func TestDiscord_Name(t *testing.T) {
	d := &Discord{}
	if got := d.Name(); got != "discord" {
		t.Errorf("Name() = %q, want %q", got, "discord")
	}
}

func TestDiscord_Send_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Discord{webhookURL: srv.URL, client: srv.Client()}
	err := d.Send(models.Event{
		Hostname: "pi",
		Severity: models.SeverityWarning,
		Message:  "test",
	})
	if err != nil {
		t.Errorf("Send() error: %v", err)
	}
}

func TestDiscord_Send_SeverityColors(t *testing.T) {
	tests := []struct {
		severity models.Severity
		color    float64
	}{
		{models.SeverityInfo, 0x3498db},
		{models.SeverityWarning, 0xf39c12},
		{models.SeverityCritical, 0xe74c3c},
	}

	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			var capturedBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			d := &Discord{webhookURL: srv.URL, client: srv.Client()}
			d.Send(models.Event{Severity: tt.severity, Hostname: "pi", Message: "test"})

			var payload map[string]interface{}
			json.Unmarshal(capturedBody, &payload)

			embeds := payload["embeds"].([]interface{})
			embed := embeds[0].(map[string]interface{})
			gotColor := embed["color"].(float64)

			if gotColor != tt.color {
				t.Errorf("color = %v, want %v", gotColor, tt.color)
			}
		})
	}
}

func TestDiscord_Send_WithDetails(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Discord{webhookURL: srv.URL, client: srv.Client()}
	d.Send(models.Event{
		Hostname:  "pi",
		Message:   "alert",
		Details:   "some details",
		Suggested: "fix it",
	})

	var payload map[string]interface{}
	json.Unmarshal(capturedBody, &payload)

	embeds := payload["embeds"].([]interface{})
	embed := embeds[0].(map[string]interface{})
	fields := embed["fields"].([]interface{})

	if len(fields) != 2 {
		t.Fatalf("got %d fields, want 2", len(fields))
	}
}

func TestDiscord_Send_WithoutDetails(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Discord{webhookURL: srv.URL, client: srv.Client()}
	d.Send(models.Event{
		Hostname: "pi",
		Message:  "alert",
	})

	var payload map[string]interface{}
	json.Unmarshal(capturedBody, &payload)

	embeds := payload["embeds"].([]interface{})
	embed := embeds[0].(map[string]interface{})
	fields := embed["fields"].([]interface{})

	if len(fields) != 0 {
		t.Errorf("got %d fields, want 0", len(fields))
	}
}

func TestDiscord_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	d := &Discord{webhookURL: srv.URL, client: srv.Client()}
	err := d.Send(models.Event{Message: "test"})
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want it to contain '400'", err.Error())
	}
}

func TestDiscord_SendRaw(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Discord{webhookURL: srv.URL, client: srv.Client()}
	if err := d.SendRaw("hello"); err != nil {
		t.Errorf("SendRaw() error: %v", err)
	}

	var payload map[string]string
	json.Unmarshal(capturedBody, &payload)
	if payload["content"] != "hello" {
		t.Errorf("content = %q, want %q", payload["content"], "hello")
	}
}

func TestDiscord_Test(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Discord{webhookURL: srv.URL, client: srv.Client()}
	if err := d.Test(); err != nil {
		t.Errorf("Test() error: %v", err)
	}

	if !strings.Contains(string(capturedBody), "PiGuard") {
		t.Error("test message should contain 'PiGuard'")
	}
}

var _ Notifier = (*Discord)(nil)
