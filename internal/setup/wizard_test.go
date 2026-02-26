package setup

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetInBlock(t *testing.T) {
	cfg := defaultConfigTemplate

	// Replace within telegram block
	result := setInBlock(cfg, "telegram", "    enabled: false", "    enabled: true")
	if !strings.Contains(result, "  telegram:\n    enabled: true") {
		t.Error("telegram block should have enabled: true")
	}
	// ntfy block should be unchanged
	if strings.Contains(result, "  ntfy:\n    enabled: true") {
		t.Error("ntfy block should still be enabled: false")
	}
}

func TestSetInBlock_NotFound(t *testing.T) {
	cfg := "some:\n  config: here\n"
	result := setInBlock(cfg, "nonexistent", "old", "new")
	if result != cfg {
		t.Error("config should be unchanged when block not found")
	}
}

func TestSetInBlock_OldNotInBlock(t *testing.T) {
	cfg := defaultConfigTemplate
	result := setInBlock(cfg, "telegram", "    nonexistent_key: value", "    new_key: value")
	if result != cfg {
		t.Error("config should be unchanged when old value not found in block")
	}
}

func TestApplyCredentials_Telegram(t *testing.T) {
	cfg := defaultConfigTemplate
	creds := wizardCreds{envVars: make(map[string]string)}

	result := applyCredentials(cfg, "telegram", creds)
	if !strings.Contains(result, "  telegram:\n    enabled: true") {
		t.Error("telegram should be enabled")
	}
	// Other notifiers should still be disabled
	if strings.Contains(result, "  ntfy:\n    enabled: true") {
		t.Error("ntfy should still be disabled")
	}
}

func TestApplyCredentials_Ntfy(t *testing.T) {
	cfg := defaultConfigTemplate
	creds := wizardCreds{
		envVars:    make(map[string]string),
		ntfyTopic:  "my-topic",
		ntfyServer: "https://my-server.example",
		ntfyToken:  "my-token",
	}

	result := applyCredentials(cfg, "ntfy", creds)
	if !strings.Contains(result, "  ntfy:\n    enabled: true") {
		t.Error("ntfy should be enabled")
	}
	if !strings.Contains(result, `"my-topic"`) {
		t.Error("ntfy topic should be set")
	}
	if !strings.Contains(result, `"https://my-server.example"`) {
		t.Error("ntfy server should be set")
	}
	if !strings.Contains(result, `"my-token"`) {
		t.Error("ntfy token should be set")
	}
}

func TestApplyCredentials_Webhook(t *testing.T) {
	cfg := defaultConfigTemplate
	creds := wizardCreds{envVars: make(map[string]string)}

	result := applyCredentials(cfg, "webhook", creds)
	if !strings.Contains(result, "  webhook:\n    enabled: true") {
		t.Error("webhook should be enabled")
	}
	if !strings.Contains(result, `"${PIGUARD_WEBHOOK_URL}"`) {
		t.Error("webhook URL should have env var placeholder")
	}
}

func TestToolInstalled(t *testing.T) {
	// /bin/sh should exist on any Unix system
	if !toolInstalled("/bin/sh") {
		t.Error("expected /bin/sh to be found")
	}
	if toolInstalled("/nonexistent/binary/path") {
		t.Error("nonexistent path should return false")
	}
	// Mix: one exists, one doesn't
	if !toolInstalled("/nonexistent/path", "/bin/sh") {
		t.Error("should return true if any path exists")
	}
}

func TestReadBool(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal bool
		want       bool
	}{
		{"y\n", false, true},
		{"yes\n", false, true},
		{"Y\n", false, true},
		{"YES\n", false, true},
		{"n\n", true, false},
		{"no\n", true, false},
		{"\n", true, true},   // empty uses default
		{"\n", false, false}, // empty uses default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tt.input))
			if got := readBool(r, tt.defaultVal); got != tt.want {
				t.Errorf("readBool(%q, %v) = %v, want %v", tt.input, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestWriteEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env")

	vars := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}

	if err := writeEnvFile(path, vars); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading env file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "KEY1=value1\n") {
		t.Errorf("missing KEY1 in output: %q", content)
	}
	if !strings.Contains(content, "KEY2=value2\n") {
		t.Errorf("missing KEY2 in output: %q", content)
	}

	// Check permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

func TestReadLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("hello world\n"))
	got := readLine(r)
	if got != "hello world" {
		t.Errorf("readLine() = %q, want %q", got, "hello world")
	}
}

func TestEnsureConfig_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.yaml")

	if err := ensureConfig(path); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created config: %v", err)
	}
	if !strings.Contains(string(data), "notifications:") {
		t.Error("created config should contain default template")
	}
}

func TestEnsureConfig_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("existing"), 0600)

	if err := ensureConfig(path); err != nil {
		t.Fatalf("ensureConfig: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "existing" {
		t.Error("existing file should not be overwritten")
	}
}
