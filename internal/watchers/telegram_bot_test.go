package watchers

import "testing"

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
	if result == "" {
		t.Error("expected confirmation message")
	}
	if !containsString(result, "confirmation") && !containsString(result, "CONFIRM") {
		t.Errorf("result should mention confirmation: %q", result)
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
	if !containsString(result, "CONFIRM") {
		t.Errorf("expected CONFIRM prompt, got: %q", result)
	}
	if !containsString(result, "nginx") {
		t.Errorf("expected container name in prompt, got: %q", result)
	}
}

func TestCmdDockerRemove_WrongKeyword(t *testing.T) {
	w := &TelegramBotWatcher{}
	// A word other than CONFIRM should not satisfy the check.
	result := w.cmdDockerRemove([]string{"nginx", "YES"})
	if !containsString(result, "CONFIRM") {
		t.Errorf("wrong keyword should not pass; expected CONFIRM prompt, got: %q", result)
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
	if !containsString(result, "CONFIRM") {
		t.Errorf("expected CONFIRM prompt, got: %q", result)
	}
}

func TestCmdDockerPrune_WithWrongKeyword(t *testing.T) {
	w := &TelegramBotWatcher{}
	// A word other than CONFIRM should not satisfy the check.
	result := w.cmdDockerPrune([]string{"YES"})
	if !containsString(result, "CONFIRM") {
		t.Errorf("wrong keyword should not pass; expected CONFIRM prompt, got: %q", result)
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
