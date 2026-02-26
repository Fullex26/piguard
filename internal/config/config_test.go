package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

const minimalValidConfig = `
notifications:
  ntfy:
    enabled: true
    topic: "test-topic"
alerts:
  min_severity: "warning"
`

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Ports.Enabled {
		t.Error("Ports.Enabled should default to true")
	}
	if cfg.Ports.Cooldown != "15m" {
		t.Errorf("Ports.Cooldown = %q, want %q", cfg.Ports.Cooldown, "15m")
	}
	if cfg.System.DiskThreshold != 80 {
		t.Errorf("System.DiskThreshold = %d, want 80", cfg.System.DiskThreshold)
	}
	if cfg.System.MemoryThreshold != 90 {
		t.Errorf("System.MemoryThreshold = %d, want 90", cfg.System.MemoryThreshold)
	}
	if len(cfg.Firewall.Chains) != 2 {
		t.Errorf("Firewall.Chains count = %d, want 2", len(cfg.Firewall.Chains))
	}
	if len(cfg.FileIntegrity.Paths) != 7 {
		t.Errorf("FileIntegrity.Paths count = %d, want 7", len(cfg.FileIntegrity.Paths))
	}
	if cfg.Alerts.MinSeverity != "warning" {
		t.Errorf("Alerts.MinSeverity = %q, want %q", cfg.Alerts.MinSeverity, "warning")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfigFile(t, minimalValidConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Notifications.Ntfy.Enabled {
		t.Error("ntfy should be enabled")
	}
	if cfg.Notifications.Ntfy.Topic != "test-topic" {
		t.Errorf("ntfy topic = %q, want %q", cfg.Notifications.Ntfy.Topic, "test-topic")
	}
}

func TestLoad_EnvVarExpansion(t *testing.T) {
	t.Setenv("PIGUARD_TEST_TOKEN", "my-secret-token")
	t.Setenv("PIGUARD_TEST_CHAT", "12345")

	yaml := `
notifications:
  telegram:
    enabled: true
    bot_token: "${PIGUARD_TEST_TOKEN}"
    chat_id: "${PIGUARD_TEST_CHAT}"
alerts:
  min_severity: "warning"
`
	path := writeConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Notifications.Telegram.BotToken != "my-secret-token" {
		t.Errorf("BotToken = %q, want %q", cfg.Notifications.Telegram.BotToken, "my-secret-token")
	}
	if cfg.Notifications.Telegram.ChatID != "12345" {
		t.Errorf("ChatID = %q, want %q", cfg.Notifications.Telegram.ChatID, "12345")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "reading config")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeConfigFile(t, "{{{{not: valid yaml at all")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "parsing config")
	}
}

func TestLoad_ValidationFailure(t *testing.T) {
	yaml := `
notifications:
  telegram:
    enabled: false
alerts:
  min_severity: "warning"
`
	path := writeConfigFile(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "invalid config")
	}
}

func TestValidate_NoNotifiers(t *testing.T) {
	cfg := DefaultConfig()
	// All notifiers are disabled by default
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when no notifiers enabled")
	}
	if !strings.Contains(err.Error(), "at least one notification channel must be enabled") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestValidate_TelegramMissingToken(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Notifications.Telegram.Enabled = true
	cfg.Notifications.Telegram.BotToken = ""
	cfg.Notifications.Telegram.ChatID = "12345"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "bot_token") {
		t.Errorf("error = %q, want it to mention bot_token", err.Error())
	}
}

func TestValidate_TelegramMissingChatID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Notifications.Telegram.Enabled = true
	cfg.Notifications.Telegram.BotToken = "token"
	cfg.Notifications.Telegram.ChatID = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing chat_id")
	}
	if !strings.Contains(err.Error(), "chat_id") {
		t.Errorf("error = %q, want it to mention chat_id", err.Error())
	}
}

func TestValidate_NtfyMissingTopic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Notifications.Ntfy.Enabled = true
	cfg.Notifications.Ntfy.Topic = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
	if !strings.Contains(err.Error(), "topic") {
		t.Errorf("error = %q, want it to mention topic", err.Error())
	}
}

func TestValidate_InvalidMinSeverity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Notifications.Ntfy.Enabled = true
	cfg.Notifications.Ntfy.Topic = "test"
	cfg.Alerts.MinSeverity = "nonsense"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
	if !strings.Contains(err.Error(), "invalid min_severity") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestValidate_ValidSeverities(t *testing.T) {
	for _, sev := range []string{"info", "warning", "critical"} {
		t.Run(sev, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Notifications.Ntfy.Enabled = true
			cfg.Notifications.Ntfy.Topic = "test"
			cfg.Alerts.MinSeverity = sev

			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() with severity %q: %v", sev, err)
			}
		})
	}
}

func TestValidate_DiscordNoExtraValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Notifications.Discord.Enabled = true
	cfg.Notifications.Discord.WebhookURL = "" // empty is fine

	if err := cfg.Validate(); err != nil {
		t.Errorf("Discord with empty URL should pass: %v", err)
	}
}

func TestHasNotifier(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Config)
		want bool
	}{
		{"none", func(c *Config) {}, false},
		{"telegram", func(c *Config) { c.Notifications.Telegram.Enabled = true }, true},
		{"ntfy", func(c *Config) { c.Notifications.Ntfy.Enabled = true }, true},
		{"discord", func(c *Config) { c.Notifications.Discord.Enabled = true }, true},
		{"webhook", func(c *Config) { c.Notifications.Webhook.Enabled = true }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.set(cfg)
			if got := cfg.HasNotifier(); got != tt.want {
				t.Errorf("HasNotifier() = %v, want %v", got, tt.want)
			}
		})
	}
}
