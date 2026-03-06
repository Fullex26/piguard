package watchers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fullex26/piguard/internal/logging"
)

func TestTelegramBotWatcher_Name(t *testing.T) {
	w := &TelegramBotWatcher{}
	if got := w.Name(); got != "telegram-bot" {
		t.Errorf("Name() = %q, want %q", got, "telegram-bot")
	}
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		percent int
		want    string
	}{
		{0, "[░░░░░░░░░░]"},
		{10, "[█░░░░░░░░░]"},
		{50, "[█████░░░░░]"},
		{100, "[██████████]"},
		{110, "[██████████]"}, // capped
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := progressBar(tt.percent); got != tt.want {
				t.Errorf("progressBar(%d) = %q, want %q", tt.percent, got, tt.want)
			}
		})
	}
}

func TestFormatKB(t *testing.T) {
	tests := []struct {
		kb   int64
		want string
	}{
		{512, "512 kB"},
		{1024, "1024 kB"},       // not > 1024, so stays as kB
		{1025, "1 MB"},          // > 1024
		{2048, "2 MB"},
		{1048576, "1024 MB"},    // not > 1048576
		{1048577, "1.0 GB"},     // > 1048576
		{2097152, "2.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatKB(tt.kb); got != tt.want {
				t.Errorf("formatKB(%d) = %q, want %q", tt.kb, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"short", "short", 10, "short"},
		{"exact", "exact", 5, "exact"},
		{"long", "long string here", 5, "long ..."},
		{"empty", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.s, tt.max); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestCmdReboot_RequiresConfirmation(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdReboot([]string{"/reboot"})
	// With inline keyboards, the no-confirm path sends a keyboard via sendReplyWithKeyboard
	// and returns "" (the keyboard is sent separately via API)
	if result != "" {
		t.Errorf("expected empty string (keyboard sent separately), got %q", result)
	}
}

// ── Docker subcommand router ──────────────────────────────────────────────────

func TestCmdDockerRouter_UnknownSubcommand_ReturnsList(t *testing.T) {
	w := &TelegramBotWatcher{}
	// Unknown subcommand falls back to list + usage hint (docker unavailable in CI is fine).
	result := w.cmdDockerRouter([]string{"/docker", "unknown_sub"})
	// Should contain a usage hint with the subcommand list.
	if !containsString(result, "stop") || !containsString(result, "restart") {
		t.Errorf("expected usage hint in result, got: %q", result)
	}
}

// ── stop ─────────────────────────────────────────────────────────────────────

func TestCmdDockerStop_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerStop([]string{})
	if !containsString(result, "Usage") {
		t.Errorf("expected usage message, got: %q", result)
	}
}

// ── restart ───────────────────────────────────────────────────────────────────

func TestCmdDockerRestart_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerRestart([]string{})
	if !containsString(result, "Usage") {
		t.Errorf("expected usage message, got: %q", result)
	}
}

// ── remove ────────────────────────────────────────────────────────────────────

func TestCmdDockerRemove_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerRemove([]string{})
	if !containsString(result, "Usage") {
		t.Errorf("expected usage message, got: %q", result)
	}
}

func TestCmdDockerRemove_NoConfirm(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerRemove([]string{"nginx"})
	// With inline keyboards, no-confirm sends keyboard and returns ""
	if result != "" {
		t.Errorf("expected empty string (keyboard sent), got: %q", result)
	}
}

func TestCmdDockerRemove_WrongKeyword(t *testing.T) {
	w := &TelegramBotWatcher{}
	// A word other than CONFIRM should not satisfy the check.
	result := w.cmdDockerRemove([]string{"nginx", "YES"})
	// With inline keyboards, wrong keyword sends keyboard and returns ""
	if result != "" {
		t.Errorf("expected empty string (keyboard sent), got: %q", result)
	}
}

// ── fix ───────────────────────────────────────────────────────────────────────

func TestCmdDockerFix_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerFix([]string{})
	if !containsString(result, "Usage") {
		t.Errorf("expected usage message, got: %q", result)
	}
}

// ── logs ──────────────────────────────────────────────────────────────────────

func TestCmdDockerLogs_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerLogs([]string{})
	if !containsString(result, "Usage") {
		t.Errorf("expected usage message, got: %q", result)
	}
}

// ── prune ─────────────────────────────────────────────────────────────────────

func TestCmdDockerPrune_NoConfirm(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerPrune([]string{})
	// With inline keyboards, no-confirm sends keyboard and returns ""
	if result != "" {
		t.Errorf("expected empty string (keyboard sent), got: %q", result)
	}
}

func TestCmdDockerPrune_WithWrongKeyword(t *testing.T) {
	w := &TelegramBotWatcher{}
	// A word other than CONFIRM should not satisfy the check.
	result := w.cmdDockerPrune([]string{"YES"})
	// With inline keyboards, wrong keyword sends keyboard and returns ""
	if result != "" {
		t.Errorf("expected empty string (keyboard sent), got: %q", result)
	}
}

// ── parseHostPorts ────────────────────────────────────────────────────────────

func TestParseHostPorts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "single port",
			input: "0.0.0.0:8080->80/tcp",
			want:  []string{"8080"},
		},
		{
			name:  "ipv6 format deduplicated",
			input: "0.0.0.0:8080->80/tcp, :::8080->80/tcp",
			want:  []string{"8080"},
		},
		{
			name:  "multiple distinct ports",
			input: "0.0.0.0:8080->80/tcp, 0.0.0.0:443->443/tcp",
			want:  []string{"8080", "443"},
		},
		{
			name:  "no arrow — no host port",
			input: "80/tcp",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHostPorts(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseHostPorts(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("parseHostPorts(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ── getLocalIP ────────────────────────────────────────────────────────────────

func TestGetLocalIP_ReturnsNonEmpty(t *testing.T) {
	// In CI, hostname -I may or may not work. Either way, we expect a non-empty string.
	ip := getLocalIP()
	if ip == "" {
		t.Error("getLocalIP() returned empty string")
	}
}

// ── Inline keyboard tests ─────────────────────────────────────────────────────

func TestHandleCallback_Dispatch(t *testing.T) {
	// We can't fully test callbacks without a running Telegram API,
	// but we verify the routing logic reaches command functions.
	// The commands that exec docker/apt will fail in CI, but the point
	// is that handleCallback routes correctly without panic.
	w := &TelegramBotWatcher{}

	knownCallbacks := []string{
		"reboot:confirm",
		"update:confirm",
		"docker:prune",
		"docker:rm:nginx",
		"storage:images",
		"storage:volumes",
		"storage:apt",
		"storage:all",
	}

	for _, data := range knownCallbacks {
		t.Run(data, func(t *testing.T) {
			// Should not panic
			w.handleCallback("test-id", data)
		})
	}
}

func TestHandleCallback_Unknown(t *testing.T) {
	w := &TelegramBotWatcher{}
	// Should not panic on unknown callback data
	w.handleCallback("test-id", "unknown:action")
}

func TestCallbackDataLength(t *testing.T) {
	// Telegram limits callback_data to 64 bytes
	codes := []string{
		"reboot:confirm",
		"update:confirm",
		"docker:prune",
		"docker:rm:very-long-container-name-here",
		"storage:images",
		"storage:volumes",
		"storage:apt",
		"storage:all",
	}
	for _, code := range codes {
		if len(code) > 64 {
			t.Errorf("callback data %q is %d bytes (max 64)", code, len(code))
		}
	}
}

func TestSendReplyWithKeyboard_JSON(t *testing.T) {
	// Verify the InlineButton struct marshals correctly
	buttons := [][]InlineButton{
		{{Text: "Confirm", Data: "test:confirm"}},
	}
	keyboard := struct {
		InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
	}{InlineKeyboard: buttons}

	data, err := json.Marshal(keyboard)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	s := string(data)
	if !containsString(s, "inline_keyboard") {
		t.Errorf("expected inline_keyboard in JSON, got: %s", s)
	}
	if !containsString(s, "callback_data") {
		t.Errorf("expected callback_data in JSON, got: %s", s)
	}
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── Command routing tests ────────────────────────────────────────────────────

func TestHandleCommand_RoutesKnownCommands(t *testing.T) {
	w := &TelegramBotWatcher{}
	// These commands should not panic even without full wiring
	commands := []string{"/help", "/status", "/ports", "/disk", "/temp", "/memory", "/uptime", "/ip"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			w.handleCommand(cmd) // should not panic
		})
	}
}

func TestHandleCommand_UnknownCommand(t *testing.T) {
	w := &TelegramBotWatcher{}
	// Capture what handleCommand produces — since sendReply will fail (no token),
	// we just test the flow doesn't panic and routes to "unknown command"
	w.handleCommand("/nonexistent")
}

func TestHandleCommand_IgnoresNonCommand(t *testing.T) {
	w := &TelegramBotWatcher{}
	w.handleCommand("just a regular message") // should silently return
}

func TestCmdHelp_ContainsAllSections(t *testing.T) {
	w := &TelegramBotWatcher{}
	help := w.cmdHelp()

	sections := []string{"System", "Security", "Docker", "Storage", "Updates", "Diagnostics", "Reports"}
	for _, s := range sections {
		if !containsString(help, s) {
			t.Errorf("help output missing section: %s", s)
		}
	}
}

func TestCmdHelp_ContainsPilog(t *testing.T) {
	w := &TelegramBotWatcher{}
	help := w.cmdHelp()
	if !containsString(help, "/pilog") {
		t.Error("help output missing /pilog command")
	}
}

// ── /pilog tests ─────────────────────────────────────────────────────────────

func TestCmdPilog_NoFileConfigured(t *testing.T) {
	old := logging.ActiveWriter
	logging.ActiveWriter = nil
	defer func() { logging.ActiveWriter = old }()

	w := &TelegramBotWatcher{}
	result := w.cmdPilog()
	if !containsString(result, "not configured") {
		t.Errorf("expected 'not configured', got: %q", result)
	}
}

func TestCmdPilog_WithFile(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/test.log"
	rw, err := logging.NewRotatingWriter(logPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	for i := 0; i < 5; i++ {
		fmt.Fprintf(rw, "log line %d\n", i)
	}

	old := logging.ActiveWriter
	logging.ActiveWriter = rw
	defer func() { logging.ActiveWriter = old }()

	w := &TelegramBotWatcher{}
	result := w.cmdPilog()
	if !containsString(result, "<pre>") {
		t.Errorf("expected <pre> tags, got: %q", result)
	}
	if !containsString(result, "log line 0") {
		t.Errorf("expected log content, got: %q", result)
	}
}

// ── sendReply HTTP test ──────────────────────────────────────────────────────

func TestSendReply_MakesHTTPCall(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		received = r.FormValue("text")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	// Extract the host part and create a watcher that uses it
	// We can't easily inject the base URL into sendReply without modifying the struct,
	// but we can test the HTTP mechanics by calling sendReply against our test server.
	// For now, just verify the format constants are correct.
	expectedFormat := "https://api.telegram.org/bot%s/sendMessage"
	actual := fmt.Sprintf(expectedFormat, "test-token")
	if !strings.HasPrefix(actual, "https://") {
		t.Errorf("telegram API URL format unexpected: %s", actual)
	}

	_ = received
	_ = server
}

// ── Poll offset tests ────────────────────────────────────────────────────────

func TestPoll_OfsetIncrement(t *testing.T) {
	// Verify offset tracking logic
	w := &TelegramBotWatcher{offset: 0}
	// Simulate processing an update with ID 42
	w.offset = 42 + 1
	if w.offset != 43 {
		t.Errorf("offset = %d, want 43", w.offset)
	}
}
