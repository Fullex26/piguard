package notifiers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/pkg/models"
)

func TestWebhook_Name(t *testing.T) {
	w := &Webhook{}
	if got := w.Name(); got != "webhook" {
		t.Errorf("Name() = %q, want %q", got, "webhook")
	}
}

func TestNewWebhook_DefaultMethod(t *testing.T) {
	w := NewWebhook(config.WebhookConfig{URL: "http://example.com"})
	if w.method != "POST" {
		t.Errorf("method = %q, want %q", w.method, "POST")
	}
}

func TestNewWebhook_CustomMethod(t *testing.T) {
	w := NewWebhook(config.WebhookConfig{URL: "http://example.com", Method: "PUT"})
	if w.method != "PUT" {
		t.Errorf("method = %q, want %q", w.method, "PUT")
	}
}

func TestWebhook_Send_Success(t *testing.T) {
	var capturedMethod string
	var capturedContentType string
	var capturedUA string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedContentType = r.Header.Get("Content-Type")
		capturedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, method: "POST", client: srv.Client()}
	err := wh.Send(models.Event{Message: "test"})
	if err != nil {
		t.Errorf("Send() error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedContentType)
	}
	if capturedUA != "PiGuard/0.1" {
		t.Errorf("User-Agent = %q, want PiGuard/0.1", capturedUA)
	}
}

func TestWebhook_Send_EventPayload(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	event := models.Event{
		ID:       "ev-1",
		Type:     models.EventPortOpened,
		Severity: models.SeverityWarning,
		Hostname: "pi",
		Message:  "test event",
		Port:     &models.PortInfo{Address: "0.0.0.0:8080"},
	}

	wh := &Webhook{url: srv.URL, method: "POST", client: srv.Client()}
	wh.Send(event)

	var got models.Event
	if err := json.Unmarshal(capturedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.ID != event.ID || got.Message != event.Message {
		t.Errorf("payload mismatch: got ID=%q Message=%q", got.ID, got.Message)
	}
	if got.Port == nil || got.Port.Address != "0.0.0.0:8080" {
		t.Error("Port payload lost")
	}
}

func TestWebhook_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, method: "POST", client: srv.Client()}
	err := wh.Send(models.Event{Message: "test"})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestWebhook_Send_CustomMethod(t *testing.T) {
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, method: "PUT", client: srv.Client()}
	wh.Send(models.Event{Message: "test"})

	if capturedMethod != "PUT" {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
}

func TestWebhook_SendRaw(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, method: "POST", client: srv.Client()}
	if err := wh.SendRaw("raw message"); err != nil {
		t.Errorf("SendRaw() error: %v", err)
	}

	var payload map[string]string
	json.Unmarshal(capturedBody, &payload)
	if payload["message"] != "raw message" {
		t.Errorf("message = %q, want %q", payload["message"], "raw message")
	}
}

func TestWebhook_Test(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, method: "POST", client: srv.Client()}
	if err := wh.Test(); err != nil {
		t.Errorf("Test() error: %v", err)
	}
	if !strings.Contains(string(capturedBody), "PiGuard") {
		t.Error("test message should contain 'PiGuard'")
	}
}

var _ Notifier = (*Webhook)(nil)
