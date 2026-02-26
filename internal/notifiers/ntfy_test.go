package notifiers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/pkg/models"
)

func TestNtfy_Name(t *testing.T) {
	n := &Ntfy{}
	if got := n.Name(); got != "ntfy" {
		t.Errorf("Name() = %q, want %q", got, "ntfy")
	}
}

func TestNewNtfy_DefaultServer(t *testing.T) {
	n := NewNtfy(config.NtfyConfig{Topic: "test"})
	if n.server != "https://ntfy.sh" {
		t.Errorf("server = %q, want %q", n.server, "https://ntfy.sh")
	}
}

func TestNewNtfy_CustomServer(t *testing.T) {
	n := NewNtfy(config.NtfyConfig{
		Topic:  "test",
		Server: "https://custom.ntfy.example",
	})
	if n.server != "https://custom.ntfy.example" {
		t.Errorf("server = %q, want %q", n.server, "https://custom.ntfy.example")
	}
}

func TestNtfy_Send_Success(t *testing.T) {
	var capturedHeaders http.Header
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "alerts", client: srv.Client()}
	err := n.Send(models.Event{
		Hostname: "pi",
		Severity: models.SeverityWarning,
		Message:  "test alert",
	})
	if err != nil {
		t.Errorf("Send() error: %v", err)
	}
	if capturedPath != "/alerts" {
		t.Errorf("path = %q, want %q", capturedPath, "/alerts")
	}
	if capturedHeaders.Get("Title") == "" {
		t.Error("missing Title header")
	}
	if capturedHeaders.Get("Priority") == "" {
		t.Error("missing Priority header")
	}
	if capturedHeaders.Get("Tags") == "" {
		t.Error("missing Tags header")
	}
}

func TestNtfy_Send_SeverityMapping(t *testing.T) {
	tests := []struct {
		severity models.Severity
		priority string
		tags     string
	}{
		{models.SeverityCritical, "urgent", "rotating_light"},
		{models.SeverityWarning, "high", "warning"},
		{models.SeverityInfo, "default", "shield"},
	}

	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			var capturedHeaders http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedHeaders = r.Header
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			n := &Ntfy{server: srv.URL, topic: "test", client: srv.Client()}
			n.Send(models.Event{Severity: tt.severity, Hostname: "pi", Message: "test"})

			if got := capturedHeaders.Get("Priority"); got != tt.priority {
				t.Errorf("priority = %q, want %q", got, tt.priority)
			}
			if got := capturedHeaders.Get("Tags"); got != tt.tags {
				t.Errorf("tags = %q, want %q", got, tt.tags)
			}
		})
	}
}

func TestNtfy_Send_WithToken(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "test", token: "secret-token", client: srv.Client()}
	n.Send(models.Event{Message: "test"})

	auth := capturedHeaders.Get("Authorization")
	if auth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer secret-token")
	}
}

func TestNtfy_Send_WithoutToken(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "test", token: "", client: srv.Client()}
	n.Send(models.Event{Message: "test"})

	if auth := capturedHeaders.Get("Authorization"); auth != "" {
		t.Errorf("Authorization = %q, want empty", auth)
	}
}

func TestNtfy_Send_RequestBody(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "test", client: srv.Client()}
	n.Send(models.Event{
		Message:   "port opened",
		Details:   "0.0.0.0:8080",
		Suggested: "close it",
	})

	if !strings.Contains(capturedBody, "port opened") {
		t.Errorf("body missing message: %q", capturedBody)
	}
	if !strings.Contains(capturedBody, "0.0.0.0:8080") {
		t.Errorf("body missing details: %q", capturedBody)
	}
	if !strings.Contains(capturedBody, "close it") {
		t.Errorf("body missing suggested: %q", capturedBody)
	}
}

func TestNtfy_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "test", client: srv.Client()}
	err := n.Send(models.Event{Message: "test"})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestNtfy_SendRaw(t *testing.T) {
	var capturedBody string
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "test", client: srv.Client()}
	if err := n.SendRaw("hello world"); err != nil {
		t.Errorf("SendRaw() error: %v", err)
	}

	if capturedHeaders.Get("Title") != "PiGuard" {
		t.Errorf("Title = %q, want %q", capturedHeaders.Get("Title"), "PiGuard")
	}
	if capturedBody != "hello world" {
		t.Errorf("body = %q, want %q", capturedBody, "hello world")
	}
}

func TestNtfy_Test(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{server: srv.URL, topic: "test", client: srv.Client()}
	if err := n.Test(); err != nil {
		t.Errorf("Test() error: %v", err)
	}
	if !strings.Contains(capturedBody, "PiGuard") {
		t.Errorf("test body should contain 'PiGuard': %q", capturedBody)
	}
}

var _ Notifier = (*Ntfy)(nil)
