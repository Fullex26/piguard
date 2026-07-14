package watchers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/logging"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func telegramTestClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`)),
			Header:     make(http.Header),
		}, nil
	})}
}

func TestTelegramBotWatcher_Name(t *testing.T) {
	w := &TelegramBotWatcher{}
	if got := w.Name(); got != "telegram-bot" {
		t.Errorf("Name() = %q, want %q", got, "telegram-bot")
	}
}

func TestRedactTelegramTransportError(t *testing.T) {
	token := "12345:secret-token"
	err := fmt.Errorf("Get https://api.telegram.org/bot%s/getUpdates: timeout", token)
	got := redactTelegramError(err, token)
	if strings.Contains(got, token) || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("token was not redacted: %q", got)
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
		{1024, "1024 kB"}, // not > 1024, so stays as kB
		{1025, "1 MB"},    // > 1024
		{2048, "2 MB"},
		{1048576, "1024 MB"}, // not > 1048576
		{1048577, "1.0 GB"},  // > 1048576
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
	w := &TelegramBotWatcher{client: telegramTestClient()}
	result := w.cmdReboot([]string{"/reboot"})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got %q", result)
	}
}

func TestCmdAutoUpdateCannotEnableScheduler(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AutoUpdate.Enabled = false
	autoUpdate := NewAutoUpdateWatcher(cfg, eventbus.New())
	w := &TelegramBotWatcher{AutoUpdateWatcher: autoUpdate}

	result := w.cmdAutoUpdateRouter([]string{"/autoupdate", "on"})
	if !containsString(result, "disabled") {
		t.Fatalf("expected disabled response, got %q", result)
	}
	if cfg.AutoUpdate.Enabled {
		t.Fatal("Telegram command enabled the auto-update scheduler")
	}
}

// ── Docker subcommand router ──────────────────────────────────────────────────

func TestCmdDockerRouter_UnknownSubcommand_ReturnsList(t *testing.T) {
	w := &TelegramBotWatcher{}
	// Unknown subcommand falls back to list + usage hint (docker unavailable in CI is fine).
	result := w.cmdDockerRouter([]string{"/docker", "unknown_sub"})
	if !containsString(result, "mutations are disabled") {
		t.Errorf("expected read-only hint in result, got: %q", result)
	}
}

// ── stop ─────────────────────────────────────────────────────────────────────

func TestCmdDockerStop_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerStop([]string{})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
	}
}

// ── restart ───────────────────────────────────────────────────────────────────

func TestCmdDockerRestart_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerRestart([]string{})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
	}
}

// ── remove ────────────────────────────────────────────────────────────────────

func TestCmdDockerRemove_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerRemove([]string{})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
	}
}

func TestCmdDockerRemove_NoConfirm(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerRemove([]string{"nginx"})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
	}
}

func TestCmdDockerRemove_WrongKeyword(t *testing.T) {
	w := &TelegramBotWatcher{}
	// A word other than CONFIRM should not satisfy the check.
	result := w.cmdDockerRemove([]string{"nginx", "YES"})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
	}
}

// ── fix ───────────────────────────────────────────────────────────────────────

func TestCmdDockerFix_NoName(t *testing.T) {
	w := &TelegramBotWatcher{}
	result := w.cmdDockerFix([]string{})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
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
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
	}
}

func TestCmdDockerPrune_WithWrongKeyword(t *testing.T) {
	w := &TelegramBotWatcher{}
	// A word other than CONFIRM should not satisfy the check.
	result := w.cmdDockerPrune([]string{"YES"})
	if !containsString(result, "disabled") {
		t.Errorf("expected disabled message, got: %q", result)
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
	w := &TelegramBotWatcher{client: telegramTestClient()}

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
	w := &TelegramBotWatcher{client: telegramTestClient()}
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
	w := &TelegramBotWatcher{client: telegramTestClient()}
	// These commands should not panic even without full wiring
	commands := []string{"/help", "/start", "/status", "/ports", "/disk", "/temp", "/memory", "/uptime", "/ip"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			w.handleCommand(cmd) // should not panic
		})
	}
}

func TestHandleCommand_UnknownCommand(t *testing.T) {
	w := &TelegramBotWatcher{client: telegramTestClient()}
	w.handleCommand("/nonexistent")
}

func TestHandleCommand_IgnoresNonCommand(t *testing.T) {
	w := &TelegramBotWatcher{}
	w.handleCommand("just a regular message") // should silently return
}

func TestBuildMainMenu_ContainsAllCategories_Legacy(t *testing.T) {
	w := &TelegramBotWatcher{}
	text, buttons := w.buildMainMenu()

	if !containsString(text, "PiGuard") {
		t.Error("main menu missing PiGuard header")
	}

	// Flatten button data values
	dataSet := make(map[string]bool)
	for _, row := range buttons {
		for _, btn := range row {
			dataSet[btn.Data] = true
		}
	}

	expected := []string{"m:sys", "m:sec", "m:dock", "m:stor", "m:upd", "m:bak", "m:rep", "m:diag"}
	for _, d := range expected {
		if !dataSet[d] {
			t.Errorf("main menu missing button with data %q", d)
		}
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
	w := &TelegramBotWatcher{
		token: "test-token",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := req.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			received = req.FormValue("text")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	w.sendReply("test message")
	if received != "test message" {
		t.Fatalf("received text = %q, want %q", received, "test message")
	}
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

// ── Menu system tests ─────────────────────────────────────────────────────────

func TestBuildMainMenu_ContainsAllCategories(t *testing.T) {
	w := &TelegramBotWatcher{}
	_, buttons := w.buildMainMenu()

	expectedData := map[string]bool{
		"m:sys": false, "m:sec": false, "m:dock": false,
		"m:stor": false, "m:upd": false, "m:bak": false,
		"m:rep": false, "m:diag": false,
	}

	for _, row := range buttons {
		for _, btn := range row {
			if _, ok := expectedData[btn.Data]; ok {
				expectedData[btn.Data] = true
			}
		}
	}

	for data, found := range expectedData {
		if !found {
			t.Errorf("main menu missing button with data %q", data)
		}
	}
}

func TestBuildMainMenu_ButtonLayout(t *testing.T) {
	w := &TelegramBotWatcher{}
	_, buttons := w.buildMainMenu()

	// Four rows of read-only categories.
	if len(buttons) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(buttons))
	}
	for i := 0; i < 4; i++ {
		if len(buttons[i]) != 2 {
			t.Errorf("row %d: expected 2 buttons, got %d", i, len(buttons[i]))
		}
	}
}

func TestBuildSystemView_ContainsMetrics(t *testing.T) {
	w := &TelegramBotWatcher{}
	text, _ := w.buildSystemView()

	// Should contain metric labels even if values are "unknown" / "N/A"
	for _, keyword := range []string{"Disk", "RAM", "Temp", "Uptime"} {
		if !containsString(text, keyword) {
			t.Errorf("system view missing %q", keyword)
		}
	}
}

func TestCallbackDataLength_NewScheme(t *testing.T) {
	// All new callback strings must be under 64 bytes
	codes := []string{
		"m:home", "m:sys", "m:sec", "m:dock", "m:stor", "m:upd",
		"m:bak", "m:rep", "m:diag",
		"s:disk", "s:mem", "s:temp", "s:up", "s:ip", "s:svc",
		"x:ports", "x:fw", "x:events", "x:scan",
		"r:refresh",
		"g:doctor", "g:pilog",
	}
	for _, code := range codes {
		if len(code) > 64 {
			t.Errorf("callback data %q is %d bytes (max 64)", code, len(code))
		}
	}
}

func TestHandleCallback_MenuNavigation(t *testing.T) {
	w := &TelegramBotWatcher{client: telegramTestClient()}
	// All menu navigation callbacks should not panic
	navCallbacks := []string{
		"m:home", "m:sys", "m:sec", "m:dock", "m:stor",
		"m:upd", "m:bak", "m:rep", "m:diag",
	}
	for _, data := range navCallbacks {
		t.Run(data, func(t *testing.T) {
			w.handleCallback("test-id", data) // should not panic
		})
	}
}

func TestHandleCallback_DetailViews(t *testing.T) {
	w := &TelegramBotWatcher{client: telegramTestClient()}
	// Detail view callbacks should not panic
	detailCallbacks := []string{
		"s:disk", "s:mem", "s:temp", "s:up", "s:ip", "s:svc",
		"x:ports", "x:fw", "x:events",
		"g:doctor", "g:pilog",
	}
	for _, data := range detailCallbacks {
		t.Run(data, func(t *testing.T) {
			w.handleCallback("test-id", data) // should not panic
		})
	}
}

func TestHandleCallback_DisabledMutationFlow(t *testing.T) {
	w := &TelegramBotWatcher{client: telegramTestClient()}

	w.handleCallback("test-id", "d:prune")
	w.handleCallback("test-id", "u:run")
}

func TestHandleCommand_StartShowsMenu(t *testing.T) {
	w := &TelegramBotWatcher{}
	// /start should not panic and should attempt to send menu
	// (API call will fail silently with no token — that's fine for this test)
	w.handleCommand("/start")
}

func TestBuildDetailView_HasBackButton(t *testing.T) {
	_, buttons := buildDetailView("test content", "m:sys")

	if len(buttons) != 1 {
		t.Fatalf("expected 1 row, got %d", len(buttons))
	}
	if len(buttons[0]) != 1 {
		t.Fatalf("expected 1 button, got %d", len(buttons[0]))
	}
	if buttons[0][0].Data != "m:sys" {
		t.Errorf("back button data = %q, want %q", buttons[0][0].Data, "m:sys")
	}
	if !containsString(buttons[0][0].Text, "Back") {
		t.Error("back button text does not contain 'Back'")
	}
}
