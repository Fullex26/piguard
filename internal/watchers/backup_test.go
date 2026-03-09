package watchers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/store"
	"github.com/Fullex26/piguard/pkg/models"
)

func newTestBackupWatcher(t *testing.T) (*BackupWatcher, chan models.Event) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{
		Backup: config.BackupConfig{
			Enabled:     true,
			Sources:     []string{"/home", "/etc"},
			Destination: "/mnt/backup/piguard",
			DayOfWeek:   "daily",
			Time:        "02:00",
			Retention:   3,
		},
	}
	bus := eventbus.New()
	received := make(chan models.Event, 10)
	bus.Subscribe(func(e models.Event) { received <- e })
	w := NewBackupWatcher(cfg, bus, db)

	// Default mocks
	w.fileExist = func(path string) bool {
		return path == "/usr/bin/rsync" || path == "/mnt/backup/piguard"
	}
	w.statPath = func(path string) (os.FileInfo, error) {
		return nil, nil
	}
	w.runRsync = func(args ...string) (string, error) {
		return "sent 1,234 bytes  received 56 bytes\ntotal size is 5,678,901\n", nil
	}
	w.runCmd = func(name string, args ...string) (string, error) {
		return "", nil
	}
	w.nowFunc = func() time.Time {
		return time.Date(2026, 3, 8, 2, 0, 0, 0, time.UTC)
	}

	return w, received
}

func awaitBackupEvent(t *testing.T, ch chan models.Event) models.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for backup event")
		return models.Event{}
	}
}

func expectNoBackupEvent(t *testing.T, ch chan models.Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("unexpected backup event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBackupWatcher_Name(t *testing.T) {
	w := &BackupWatcher{}
	if w.Name() != "backup" {
		t.Fatalf("expected 'backup', got %s", w.Name())
	}
}

func TestBackupWatcher_ScheduleMatch_Daily(t *testing.T) {
	w, _ := newTestBackupWatcher(t)

	tests := []struct {
		name   string
		now    time.Time
		expect bool
	}{
		{"match", time.Date(2026, 3, 8, 2, 0, 0, 0, time.UTC), true},
		{"wrong time", time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC), false},
		{"match different day", time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.isScheduleMatch(tt.now)
			if got != tt.expect {
				t.Errorf("isScheduleMatch() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestBackupWatcher_ScheduleMatch_Weekly(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			Enabled:   true,
			DayOfWeek: "sunday",
			Time:      "02:00",
		},
	}
	bus := eventbus.New()
	w := NewBackupWatcher(cfg, bus, nil)

	// 2026-03-08 is a Sunday
	if !w.isScheduleMatch(time.Date(2026, 3, 8, 2, 0, 0, 0, time.UTC)) {
		t.Error("expected match on Sunday at 02:00")
	}
	// Monday
	if w.isScheduleMatch(time.Date(2026, 3, 9, 2, 0, 0, 0, time.UTC)) {
		t.Error("should not match on Monday")
	}
}

func TestBackupWatcher_SuccessfulBackup(t *testing.T) {
	w, ch := newTestBackupWatcher(t)

	result := w.RunBackup()

	// Should get started + completed events
	e1 := awaitBackupEvent(t, ch)
	if e1.Type != models.EventBackupStarted {
		t.Errorf("expected backup.started, got %s", e1.Type)
	}

	e2 := awaitBackupEvent(t, ch)
	if e2.Type != models.EventBackupCompleted {
		t.Errorf("expected backup.completed, got %s", e2.Type)
	}

	if !strings.HasPrefix(result, "✅") {
		t.Errorf("expected success result, got: %s", result)
	}

	// Check persisted state
	status, _ := w.store.GetState("backup.last_status")
	if status != "success" {
		t.Errorf("expected status 'success', got %q", status)
	}
}

func TestBackupWatcher_RsyncFailure(t *testing.T) {
	w, ch := newTestBackupWatcher(t)
	w.runRsync = func(args ...string) (string, error) {
		return "rsync: connection refused", fmt.Errorf("exit status 1")
	}

	result := w.RunBackup()

	_ = awaitBackupEvent(t, ch) // started
	e := awaitBackupEvent(t, ch)
	if e.Type != models.EventBackupFailed {
		t.Errorf("expected backup.failed, got %s", e.Type)
	}

	if !strings.HasPrefix(result, "❌") {
		t.Errorf("expected failure result, got: %s", result)
	}

	status, _ := w.store.GetState("backup.last_status")
	if status != "failed" {
		t.Errorf("expected status 'failed', got %q", status)
	}
}

func TestBackupWatcher_NoRsync(t *testing.T) {
	w, ch := newTestBackupWatcher(t)
	w.fileExist = func(path string) bool {
		return false // rsync not found
	}

	result := w.RunBackup()

	e := awaitBackupEvent(t, ch) // started
	if e.Type != models.EventBackupStarted {
		t.Errorf("expected backup.started, got %s", e.Type)
	}

	e = awaitBackupEvent(t, ch) // failed
	if e.Type != models.EventBackupFailed {
		t.Errorf("expected backup.failed, got %s", e.Type)
	}

	if !strings.Contains(result, "rsync not installed") {
		t.Errorf("expected rsync not installed message, got: %s", result)
	}
}

func TestBackupWatcher_DestNotMounted(t *testing.T) {
	w, ch := newTestBackupWatcher(t)
	w.fileExist = func(path string) bool {
		return path == "/usr/bin/rsync" // dest is NOT found
	}

	result := w.RunBackup()

	_ = awaitBackupEvent(t, ch) // started
	e := awaitBackupEvent(t, ch)
	if e.Type != models.EventBackupFailed {
		t.Errorf("expected backup.failed, got %s", e.Type)
	}

	if !strings.Contains(result, "not found or not mounted") {
		t.Errorf("expected mount error, got: %s", result)
	}
}

func TestBackupWatcher_AlreadyRunning(t *testing.T) {
	w, _ := newTestBackupWatcher(t)

	// Simulate already running
	w.running.Store(true)

	result := w.RunBackup()
	if !strings.Contains(result, "already running") {
		t.Errorf("expected already running message, got: %s", result)
	}
}

func TestBackupWatcher_RetentionCleanup(t *testing.T) {
	w, ch := newTestBackupWatcher(t)

	// Create a temp dir with date-stamped subdirs
	tmpDir := t.TempDir()
	w.Cfg.Backup.Destination = tmpDir
	w.Cfg.Backup.Retention = 2

	// Create 4 backup dirs
	for _, d := range []string{"2026-03-05", "2026-03-06", "2026-03-07", "2026-03-08"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	w.fileExist = func(path string) bool {
		if path == "/usr/bin/rsync" {
			return true
		}
		_, err := os.Stat(path)
		return err == nil
	}

	w.RunBackup()

	// Drain events
	_ = awaitBackupEvent(t, ch)
	_ = awaitBackupEvent(t, ch)

	// Should keep only 2 newest dirs (2026-03-07, 2026-03-08)
	entries, _ := os.ReadDir(tmpDir)
	dateDirs := 0
	for _, e := range entries {
		if e.IsDir() && isDateDir(e.Name()) {
			dateDirs++
		}
	}
	if dateDirs != 2 {
		t.Errorf("expected 2 retained backups, got %d", dateDirs)
	}

	// Verify the oldest ones were removed
	if _, err := os.Stat(filepath.Join(tmpDir, "2026-03-05")); err == nil {
		t.Error("expected 2026-03-05 to be removed")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "2026-03-06")); err == nil {
		t.Error("expected 2026-03-06 to be removed")
	}
}

func TestBackupWatcher_RemoteDestination(t *testing.T) {
	w, ch := newTestBackupWatcher(t)
	w.Cfg.Backup.Destination = "user@host:/backup/pi"

	var rsyncArgs []string
	w.runRsync = func(args ...string) (string, error) {
		rsyncArgs = args
		return "total size is 1000\n", nil
	}
	w.runCmd = func(name string, args ...string) (string, error) {
		return "", nil // SSH connectivity check passes
	}
	w.fileExist = func(path string) bool {
		return path == "/usr/bin/rsync"
	}

	w.RunBackup()

	_ = awaitBackupEvent(t, ch) // started
	_ = awaitBackupEvent(t, ch) // completed

	// Check that SSH transport flag was added
	found := false
	for _, arg := range rsyncArgs {
		if strings.Contains(arg, "ssh") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SSH transport in rsync args: %v", rsyncArgs)
	}

	// Check destination includes date dir
	lastArg := rsyncArgs[len(rsyncArgs)-1]
	if !strings.HasPrefix(lastArg, "user@host:") {
		t.Errorf("expected remote destination, got: %s", lastArg)
	}
	if !strings.Contains(lastArg, "2026-03-08") {
		t.Errorf("expected date dir in destination, got: %s", lastArg)
	}
}

func TestBackupWatcher_CustomRsyncFlags(t *testing.T) {
	w, ch := newTestBackupWatcher(t)
	w.Cfg.Backup.RsyncFlags = "-av --exclude=*.log"

	var rsyncArgs []string
	w.runRsync = func(args ...string) (string, error) {
		rsyncArgs = args
		return "total size is 1000\n", nil
	}

	w.RunBackup()

	_ = awaitBackupEvent(t, ch)
	_ = awaitBackupEvent(t, ch)

	argsStr := strings.Join(rsyncArgs, " ")
	if !strings.Contains(argsStr, "--exclude=*.log") {
		t.Errorf("expected custom rsync flags, got: %v", rsyncArgs)
	}
}

func TestBackupWatcher_GetStatus_NoBackups(t *testing.T) {
	w, _ := newTestBackupWatcher(t)

	status := w.GetStatus()
	if !strings.Contains(status, "No backups have run yet") {
		t.Errorf("expected no backups message, got: %s", status)
	}
}

func TestBackupWatcher_GetStatus_AfterSuccess(t *testing.T) {
	w, ch := newTestBackupWatcher(t)

	w.RunBackup()
	_ = awaitBackupEvent(t, ch)
	_ = awaitBackupEvent(t, ch)

	status := w.GetStatus()
	if !strings.Contains(status, "success") {
		t.Errorf("expected success in status, got: %s", status)
	}
	if !strings.Contains(status, "2026-03-08") {
		t.Errorf("expected date in status, got: %s", status)
	}
}

func TestIsDateDir(t *testing.T) {
	tests := map[string]bool{
		"2026-03-08": true,
		"2026-12-31": true,
		"latest":     false,
		"backup":     false,
		"2026-3-8":   false,
		"20260308":   false,
	}
	for input, expected := range tests {
		if got := isDateDir(input); got != expected {
			t.Errorf("isDateDir(%q) = %v, want %v", input, got, expected)
		}
	}
}

func TestParseRsyncSize(t *testing.T) {
	tests := []struct {
		name   string
		output string
		expect string
	}{
		{"total size line", "total size is 5,678,901\n", "5.4 MB"},
		{"sent line", "sent 1,234 bytes  received 56 bytes\n", "1.2 KB"},
		{"empty output", "", ""},
		{"no match", "some other output\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRsyncSize(tt.output)
			if got != tt.expect {
				t.Errorf("parseRsyncSize() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := map[string]string{
		"500":        "500 B",
		"1024":       "1.0 KB",
		"1048576":    "1.0 MB",
		"1073741824": "1.0 GB",
	}
	for input, expected := range tests {
		if got := formatBytes(input); got != expected {
			t.Errorf("formatBytes(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d      time.Duration
		expect string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{2*time.Hour + 15*time.Minute, "2h15m"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.expect {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.expect)
		}
	}
}
