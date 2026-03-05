package doctor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Fullex26/piguard/internal/config"
)

// ── mock DB ──────────────────────────────────────────────────────────────────

type mockDB struct {
	count     int
	lastAlert string
	err       error
}

func (m *mockDB) GetEventCount(_ int) (int, error) { return m.count, m.err }
func (m *mockDB) GetLastAlertTime() (string, error) { return m.lastAlert, m.err }
func (m *mockDB) Close() error                      { return nil }

// ── test helpers ─────────────────────────────────────────────────────────────

func minimalCfg() *config.Config {
	return &config.Config{
		Notifications: config.NotificationConfig{
			Telegram: config.TelegramConfig{Enabled: true, BotToken: "x", ChatID: "1"},
		},
	}
}

// stubRunner returns a runner with real config but all OS calls stubbed.
func stubRunner(cfg *config.Config, execResults map[string]struct {
	out  string
	code int
}, db dbQuerier) *Runner {
	r := New(cfg, "/tmp/test.db")
	r.execFn = func(name string, args ...string) (string, int) {
		key := name
		if res, ok := execResults[key]; ok {
			return res.out, res.code
		}
		return "", -1 // default: not found
	}
	r.existsFn = func(_ string) bool { return false }
	r.writableFn = func(_ string) bool { return true }
	r.openDB = func(_ string) (dbQuerier, error) {
		if db != nil {
			return db, nil
		}
		return nil, fmt.Errorf("no db")
	}
	return r
}

// ── checkConfig ───────────────────────────────────────────────────────────────

func TestCheckConfig_Nil(t *testing.T) {
	r := &Runner{}
	res := r.checkConfig()
	if res.Status != StatusFail {
		t.Errorf("nil config: want Fail, got %v", res.Status)
	}
}

func TestCheckConfig_OK(t *testing.T) {
	r := &Runner{cfg: minimalCfg()}
	res := r.checkConfig()
	if res.Status != StatusOK {
		t.Errorf("valid config: want OK, got %v", res.Status)
	}
}

// ── checkNotifiers ────────────────────────────────────────────────────────────

func TestCheckNotifiers_None(t *testing.T) {
	r := &Runner{cfg: &config.Config{}}
	res := r.checkNotifiers()
	if res.Status != StatusFail {
		t.Errorf("no notifiers: want Fail, got %v", res.Status)
	}
}

func TestCheckNotifiers_Telegram(t *testing.T) {
	r := &Runner{cfg: minimalCfg()}
	res := r.checkNotifiers()
	if res.Status != StatusOK {
		t.Errorf("telegram enabled: want OK, got %v", res.Status)
	}
	if !strings.Contains(res.Message, "Telegram") {
		t.Errorf("message should mention Telegram, got: %q", res.Message)
	}
}

func TestCheckNotifiers_Multiple(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationConfig{
			Telegram: config.TelegramConfig{Enabled: true},
			Ntfy:     config.NtfyConfig{Enabled: true},
		},
	}
	r := &Runner{cfg: cfg}
	res := r.checkNotifiers()
	if !strings.Contains(res.Message, "Telegram") || !strings.Contains(res.Message, "ntfy") {
		t.Errorf("message should list all notifiers, got: %q", res.Message)
	}
}

// ── checkDaemon ───────────────────────────────────────────────────────────────

func TestCheckDaemon_Active(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"systemctl": {"active", 0},
	}, &mockDB{})
	res := r.checkDaemon()
	if res.Status != StatusOK {
		t.Errorf("want OK, got %v: %s", res.Status, res.Message)
	}
}

func TestCheckDaemon_Inactive(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"systemctl": {"inactive", 3},
	}, &mockDB{})
	res := r.checkDaemon()
	if res.Status != StatusFail {
		t.Errorf("want Fail, got %v", res.Status)
	}
	if res.Fix == "" {
		t.Error("inactive service should have a fix")
	}
}

func TestCheckDaemon_NoSystemd(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{}, &mockDB{})
	// execFn returns -1 for unknown commands (systemctl not found)
	res := r.checkDaemon()
	if res.Status != StatusSkip {
		t.Errorf("no systemd: want Skip, got %v", res.Status)
	}
}

// ── checkEventStore ───────────────────────────────────────────────────────────

func TestCheckEventStore_DBOpen_OK(t *testing.T) {
	r := stubRunner(minimalCfg(), nil, &mockDB{count: 5, lastAlert: "10m ago"})
	res := r.checkEventStore()
	if res.Status != StatusOK {
		t.Errorf("want OK, got %v: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "5 events") {
		t.Errorf("message should show event count, got: %q", res.Message)
	}
}

func TestCheckEventStore_DBFail(t *testing.T) {
	r := stubRunner(minimalCfg(), nil, nil) // nil db → openDB returns error
	res := r.checkEventStore()
	if res.Status != StatusFail {
		t.Errorf("want Fail, got %v", res.Status)
	}
}

// ── checkSS ───────────────────────────────────────────────────────────────────

func TestCheckSS_Found(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"ss": {"ss, iproute2-5.10.0", 0},
	}, &mockDB{})
	if res := r.checkSS(); res.Status != StatusOK {
		t.Errorf("want OK, got %v", res.Status)
	}
}

func TestCheckSS_NotFound(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{}, &mockDB{})
	res := r.checkSS()
	if res.Status != StatusFail {
		t.Errorf("want Fail, got %v", res.Status)
	}
	if res.Fix == "" {
		t.Error("missing ss should have a fix")
	}
}

// ── checkIPTables ─────────────────────────────────────────────────────────────

func TestCheckIPTables_OK(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"iptables": {"Chain INPUT (policy DROP)", 0},
	}, &mockDB{})
	if res := r.checkIPTables(); res.Status != StatusOK {
		t.Errorf("want OK, got %v", res.Status)
	}
}

func TestCheckIPTables_PermissionDenied(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"iptables": {"Permission denied (you must be root)", 4},
	}, &mockDB{})
	res := r.checkIPTables()
	if res.Status != StatusWarn {
		t.Errorf("want Warn, got %v", res.Status)
	}
	if res.Fix == "" {
		t.Error("permission denied should have a fix")
	}
}

func TestCheckIPTables_NotFound(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{}, &mockDB{})
	if res := r.checkIPTables(); res.Status != StatusFail {
		t.Errorf("want Fail, got %v", res.Status)
	}
}

// ── checkDocker ───────────────────────────────────────────────────────────────

func TestCheckDocker_OK(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"docker": {"5", 0},
	}, &mockDB{})
	res := r.checkDocker()
	if res.Status != StatusOK {
		t.Errorf("want OK, got %v", res.Status)
	}
	if !strings.Contains(res.Message, "5 containers") {
		t.Errorf("message should include container count, got: %q", res.Message)
	}
}

func TestCheckDocker_PermissionDenied(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"docker": {"permission denied while trying to connect", 1},
	}, &mockDB{})
	res := r.checkDocker()
	if res.Status != StatusWarn {
		t.Errorf("want Warn, got %v", res.Status)
	}
}

func TestCheckDocker_NotFound(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{}, &mockDB{})
	if res := r.checkDocker(); res.Status != StatusFail {
		t.Errorf("want Fail, got %v", res.Status)
	}
}

// ── checkRKHunter ─────────────────────────────────────────────────────────────

func TestCheckRKHunter_NotInstalled(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{}, &mockDB{})
	res := r.checkRKHunter()
	if res.Status != StatusWarn {
		t.Errorf("want Warn, got %v", res.Status)
	}
}

func TestCheckRKHunter_InstalledLogMissing(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"rkhunter": {"rkhunter 1.4.6", 0},
	}, &mockDB{})
	// existsFn returns false (default in stubRunner) → log doesn't exist → OK
	res := r.checkRKHunter()
	if res.Status != StatusOK {
		t.Errorf("want OK when log missing (first run), got %v", res.Status)
	}
}

func TestCheckRKHunter_LogNotWritable(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"rkhunter": {"rkhunter 1.4.6", 0},
	}, &mockDB{})
	r.existsFn = func(_ string) bool { return true }    // log exists
	r.writableFn = func(_ string) bool { return false } // but not writable
	res := r.checkRKHunter()
	if res.Status != StatusWarn {
		t.Errorf("want Warn, got %v", res.Status)
	}
	if !strings.Contains(res.Fix, "chmod") {
		t.Errorf("fix should mention chmod, got: %q", res.Fix)
	}
}

// ── checkClamAV ───────────────────────────────────────────────────────────────

func TestCheckClamAV_NotInstalled(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{}, &mockDB{})
	if res := r.checkClamAV(); res.Status != StatusWarn {
		t.Errorf("want Warn, got %v", res.Status)
	}
}

func TestCheckClamAV_Installed(t *testing.T) {
	r := stubRunner(minimalCfg(), map[string]struct{ out string; code int }{
		"clamscan": {"ClamAV 1.0.0", 0},
	}, &mockDB{})
	if res := r.checkClamAV(); res.Status != StatusOK {
		t.Errorf("want OK, got %v", res.Status)
	}
}

// ── disabled-watcher skips ────────────────────────────────────────────────────

func TestRun_DisabledWatchersAreSkipped(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationConfig{
			Telegram: config.TelegramConfig{Enabled: true},
		},
		Firewall:      config.FirewallConfig{Enabled: false},
		Docker:        config.DockerConfig{Enabled: false},
		SecurityTools: config.SecurityToolsConfig{Enabled: false},
		Network:       config.NetworkConfig{Enabled: false},
	}
	r := stubRunner(cfg, map[string]struct{ out string; code int }{
		"systemctl": {"active", 0},
		"ss":        {"iproute2", 0},
	}, &mockDB{count: 1})

	results := r.Run()

	checkName := func(name string) CheckResult {
		for _, res := range results {
			if res.Name == name {
				return res
			}
		}
		t.Fatalf("check %q not found in results", name)
		return CheckResult{}
	}

	for _, name := range []string{"iptables", "docker", "rkhunter", "ClamAV", "ip"} {
		if res := checkName(name); res.Status != StatusSkip {
			t.Errorf("disabled watcher %q: want Skip, got %v", name, res.Status)
		}
	}
}

// ── renderers ─────────────────────────────────────────────────────────────────

func TestRenderCLI_AllPassed(t *testing.T) {
	results := []CheckResult{
		{Category: "Config", Name: "Config file", Status: StatusOK, Message: "Loaded"},
		{Category: "Config", Name: "Notifiers", Status: StatusOK, Message: "Telegram"},
	}
	out := RenderCLI(results)
	if !strings.Contains(out, "All 2 checks passed") {
		t.Errorf("expected all-passed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "✅") {
		t.Error("expected ✅ in output")
	}
}

func TestRenderCLI_WithFailure(t *testing.T) {
	results := []CheckResult{
		{Category: "Config", Name: "Config file", Status: StatusOK, Message: "Loaded"},
		{Category: "Config", Name: "Notifiers", Status: StatusFail, Message: "None enabled", Fix: "enable one"},
	}
	out := RenderCLI(results)
	if !strings.Contains(out, "1 failed") {
		t.Errorf("expected failure summary, got:\n%s", out)
	}
	if !strings.Contains(out, "enable one") {
		t.Error("expected fix hint in output")
	}
}

func TestRenderCLI_WithWarn(t *testing.T) {
	results := []CheckResult{
		{Category: "Dependencies", Name: "rkhunter", Status: StatusWarn, Message: "Log not writable", Fix: "chmod 666"},
	}
	out := RenderCLI(results)
	if !strings.Contains(out, "1 checks need attention") {
		t.Errorf("expected warn summary, got:\n%s", out)
	}
}

func TestRenderTelegram_AllPassed(t *testing.T) {
	results := []CheckResult{
		{Category: "Config", Name: "Config file", Status: StatusOK, Message: "Loaded"},
	}
	out := RenderTelegram(results)
	if !strings.Contains(out, "All 1 checks passed") {
		t.Errorf("expected passed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "<b>") {
		t.Error("expected HTML bold tags in telegram output")
	}
}

func TestRenderTelegram_WithFix(t *testing.T) {
	results := []CheckResult{
		{Category: "Dependencies", Name: "rkhunter", Status: StatusWarn,
			Message: "Log not writable", Fix: "sudo chmod 666 /var/log/rkhunter.log"},
	}
	out := RenderTelegram(results)
	if !strings.Contains(out, "<code>") {
		t.Error("fix should be wrapped in <code> tags")
	}
	if !strings.Contains(out, "chmod 666") {
		t.Error("fix command should appear in output")
	}
}

func TestRenderCLI_SkipNoFix(t *testing.T) {
	results := []CheckResult{
		{Category: "Dependencies", Name: "docker", Status: StatusSkip, Message: "Disabled"},
	}
	out := RenderCLI(results)
	// Skip entries must not print a Fix line even if Fix is set
	if strings.Contains(out, "↳") {
		t.Error("skipped checks should not show fix hint")
	}
}
