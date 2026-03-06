package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Fullex26/piguard/internal/config"
)

func TestSetup_StderrOnly(t *testing.T) {
	rw, err := Setup(config.LoggingConfig{Level: "info"}, false)
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	if rw != nil {
		t.Error("expected nil RotatingWriter when no file configured")
	}
}

func TestSetup_FileAndStderr(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "test.log")
	rw, err := Setup(config.LoggingConfig{Level: "info", File: logPath, MaxSizeMB: 1}, false)
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	if rw == nil {
		t.Fatal("expected non-nil RotatingWriter")
	}
	defer rw.Close()

	slog.Info("test message from logging_test")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), "test message from logging_test") {
		t.Errorf("log file does not contain test message, got: %s", string(data))
	}
}

func TestSetup_VerboseOverride(t *testing.T) {
	rw, err := Setup(config.LoggingConfig{Level: "error"}, true)
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	if rw != nil {
		rw.Close()
	}
	// Verify debug-level logging is enabled by checking the handler accepts debug
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		t.Error("verbose=true should enable debug level")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseLevel(tt.input); got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRotatingWriter_RotatesAtMaxSize(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rotate.log")

	// 1 KB max size for easy testing
	rw, err := NewRotatingWriter(logPath, 0)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	// Override maxBytes to 100 bytes for test
	rw.maxBytes = 100
	defer rw.Close()

	// Write enough to trigger rotation
	line := strings.Repeat("a", 60) + "\n"
	rw.Write([]byte(line)) // 61 bytes
	rw.Write([]byte(line)) // 122 bytes -> triggers rotation on this write

	// Backup file should exist
	if _, err := os.Stat(logPath + ".1"); os.IsNotExist(err) {
		t.Error("expected backup file .1 to exist after rotation")
	}
}

func TestRotatingWriter_BackupOverwrite(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rotate.log")

	rw, err := NewRotatingWriter(logPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	rw.maxBytes = 50
	defer rw.Close()

	// First write triggers rotation (0+61 > 50), writes A to fresh file
	rw.Write([]byte(strings.Repeat("A", 60) + "\n"))
	// Second write triggers rotation (61+61 > 50), A goes to .1, B written fresh
	rw.Write([]byte(strings.Repeat("B", 60) + "\n"))

	// Backup .1 should contain A data (rotated out when B was written)
	backup, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(backup), "AAAAA") {
		t.Errorf("backup should contain A data, got: %s", string(backup))
	}

	// Current file should contain B data
	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "BBBBB") {
		t.Errorf("current file should contain B data, got: %s", string(current))
	}
}

func TestTailLines_Normal(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tail.log")

	rw, err := NewRotatingWriter(logPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	for i := 0; i < 10; i++ {
		rw.Write([]byte("line\n"))
	}

	tail, err := rw.TailLines(5)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(tail, "\n")
	if len(lines) != 5 {
		t.Errorf("TailLines(5) returned %d lines, want 5", len(lines))
	}
}

func TestTailLines_FewerThanN(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tail.log")

	rw, err := NewRotatingWriter(logPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	rw.Write([]byte("line1\nline2\n"))

	tail, err := rw.TailLines(10)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(tail, "\n")
	if len(lines) != 2 {
		t.Errorf("TailLines(10) with 2 lines returned %d lines, want 2", len(lines))
	}
}

func TestTailLines_Empty(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "empty.log")

	rw, err := NewRotatingWriter(logPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	tail, err := rw.TailLines(5)
	if err != nil {
		t.Fatal(err)
	}
	if tail != "" {
		t.Errorf("TailLines on empty file = %q, want empty", tail)
	}
}

func TestRotatingWriter_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "concurrent.log")

	rw, err := NewRotatingWriter(logPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rw.Write([]byte("concurrent write\n"))
		}()
	}
	wg.Wait()

	// Just verify no race/panic — the -race flag catches races
}
