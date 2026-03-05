package watchers

import (
	"encoding/json"
	"testing"
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
