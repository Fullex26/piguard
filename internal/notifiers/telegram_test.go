package notifiers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Fullex26/piguard/pkg/models"
)

func TestTelegram_Name(t *testing.T) {
	tg := &Telegram{}
	if got := tg.Name(); got != "telegram" {
		t.Errorf("Name() = %q, want %q", got, "telegram")
	}
}

func TestTelegram_Send_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tg := &Telegram{
		token:  "test-token",
		chatID: "12345",
		client: srv.Client(),
	}
	// Override the API URL by using a token that encodes the server URL
	// The format will be: srv.URL/bottest-token/sendMessage
	// We need to override the URL construction — use the test server as the token prefix
	// Instead, let's intercept at the transport level
	tg.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) *http.Response {
			// Forward to test server
			req2, _ := http.NewRequest(req.Method, srv.URL+req.URL.Path, req.Body)
			req2.Header = req.Header
			resp, _ := srv.Client().Do(req2)
			return resp
		}),
	}

	event := models.Event{
		Hostname: "pi",
		Severity: models.SeverityWarning,
		Message:  "test alert",
	}

	if err := tg.Send(event); err != nil {
		t.Errorf("Send() error: %v", err)
	}
}

func TestTelegram_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tg := &Telegram{
		token:  "test-token",
		chatID: "12345",
		client: &http.Client{
			Transport: redirectTransport(srv.URL),
		},
	}

	err := tg.Send(models.Event{Message: "test"})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to contain '500'", err.Error())
	}
}

func TestTelegram_Send_FormData(t *testing.T) {
	var capturedBody string
	var capturedContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tg := &Telegram{
		token:  "test-token",
		chatID: "99999",
		client: &http.Client{Transport: redirectTransport(srv.URL)},
	}

	tg.Send(models.Event{
		Hostname: "pi",
		Severity: models.SeverityInfo,
		Message:  "test msg",
	})

	if !strings.Contains(capturedContentType, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want form-urlencoded", capturedContentType)
	}
	if !strings.Contains(capturedBody, "chat_id=99999") {
		t.Errorf("body missing chat_id: %q", capturedBody)
	}
	if !strings.Contains(capturedBody, "parse_mode=HTML") {
		t.Errorf("body missing parse_mode: %q", capturedBody)
	}
}

func TestTelegram_SendRaw(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tg := &Telegram{
		token:  "test-token",
		chatID: "12345",
		client: &http.Client{Transport: redirectTransport(srv.URL)},
	}

	if err := tg.SendRaw("hello world"); err != nil {
		t.Errorf("SendRaw() error: %v", err)
	}
	if !strings.Contains(capturedBody, "hello+world") && !strings.Contains(capturedBody, "hello%20world") && !strings.Contains(capturedBody, "hello world") {
		t.Errorf("body doesn't contain message: %q", capturedBody)
	}
}

func TestTelegram_Test(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tg := &Telegram{
		token:  "test-token",
		chatID: "12345",
		client: &http.Client{Transport: redirectTransport(srv.URL)},
	}

	if err := tg.Test(); err != nil {
		t.Errorf("Test() error: %v", err)
	}
	if !strings.Contains(capturedBody, "PiGuard") {
		t.Errorf("test message should contain 'PiGuard': %q", capturedBody)
	}
}

func TestTelegram_formatEvent(t *testing.T) {
	tg := &Telegram{}
	event := models.Event{
		Hostname:  "pi",
		Severity:  models.SeverityWarning,
		Message:   "Port opened",
		Details:   "0.0.0.0:8080",
		Suggested: "Close it",
	}

	result := tg.formatEvent(event)
	for _, want := range []string{"pi", "Port opened", "0.0.0.0:8080", "Close it"} {
		if !strings.Contains(result, want) {
			t.Errorf("formatEvent() missing %q in result: %q", want, result)
		}
	}
}

func TestFormatDailySummary(t *testing.T) {
	health := models.SystemHealth{
		DiskUsagePercent:  50,
		MemoryUsedPercent: 60,
		CPUTempCelsius:    45.0,
		ContainersRunning: 3,
		ListeningPorts:    5,
	}

	result := FormatDailySummary("pi", health, "2 hours ago")
	for _, want := range []string{"pi", "50%", "60%", "45°C", "3 running", "5", "2 hours ago"} {
		if !strings.Contains(result, want) {
			t.Errorf("FormatDailySummary() missing %q", want)
		}
	}
}

func TestFormatDailySummary_NoTemp(t *testing.T) {
	health := models.SystemHealth{
		DiskUsagePercent:  50,
		MemoryUsedPercent: 60,
		CPUTempCelsius:    0,
		ListeningPorts:    5,
	}

	result := FormatDailySummary("pi", health, "never")
	if strings.Contains(result, "Temp:") {
		t.Error("should omit temperature when 0")
	}
}

func TestFormatDailySummary_NoContainers(t *testing.T) {
	health := models.SystemHealth{
		DiskUsagePercent:  50,
		MemoryUsedPercent: 60,
		ContainersRunning: 0,
		ListeningPorts:    5,
	}

	result := FormatDailySummary("pi", health, "")
	if strings.Contains(result, "Containers:") {
		t.Error("should omit containers when 0")
	}
}

func TestFormatDailySummary_EmptyLastAlert(t *testing.T) {
	health := models.SystemHealth{
		DiskUsagePercent:  50,
		MemoryUsedPercent: 60,
		ListeningPorts:    5,
	}

	result := FormatDailySummary("pi", health, "")
	if strings.Contains(result, "Last alert:") {
		t.Error("should omit last alert when empty")
	}
}

// -- helpers --

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

// redirectTransport creates a transport that redirects all requests to the given base URL.
func redirectTransport(baseURL string) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) *http.Response {
		newURL := baseURL + req.URL.Path
		req2, _ := http.NewRequest(req.Method, newURL, req.Body)
		req2.Header = req.Header
		resp, err := http.DefaultTransport.RoundTrip(req2)
		if err != nil {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader(err.Error())),
			}
		}
		return resp
	})
}

// Ensure Telegram implements Notifier at compile time (checked in notifier_test.go too).
var _ Notifier = (*Telegram)(nil)

// Unused but prevents "imported and not used" if time is only used in imports.
var _ = time.Second
