package doctor

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"strings"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/store"
)

// Status is the outcome of a single doctor check.
type Status int

const (
	StatusOK   Status = iota
	StatusWarn        // something is off but piguard still runs
	StatusFail        // piguard cannot function correctly
	StatusSkip        // feature disabled — check not applicable
)

func (s Status) Emoji() string {
	switch s {
	case StatusOK:
		return "✅"
	case StatusWarn:
		return "⚠️"
	case StatusFail:
		return "❌"
	default:
		return "— "
	}
}

// CheckResult holds the outcome of one doctor check.
type CheckResult struct {
	Category string
	Name     string
	Status   Status
	Message  string
	Fix      string // optional one-liner remedy shown on failure/warn
}

// dbQuerier is the subset of store.Store that Doctor needs.
type dbQuerier interface {
	GetEventCount(hours int) (int, error)
	GetLastAlertTime() (string, error)
	Close() error
}

// Runner executes all doctor checks.
type Runner struct {
	cfg        *config.Config
	dbPath     string
	execFn     func(name string, args ...string) (string, int) // output + exit code
	existsFn   func(path string) bool
	writableFn func(path string) bool
	openDB     func(path string) (dbQuerier, error)
}

// New creates a Runner wired to real OS calls.
func New(cfg *config.Config, dbPath string) *Runner {
	r := &Runner{cfg: cfg, dbPath: dbPath}
	r.execFn = defaultExec
	r.existsFn = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	r.writableFn = func(path string) bool {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return false
		}
		f.Close()
		return true
	}
	r.openDB = func(path string) (dbQuerier, error) {
		return store.Open(path)
	}
	return r
}

func defaultExec(name string, args ...string) (string, int) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return strings.TrimSpace(string(out)), exitErr.ExitCode()
		}
		return "", -1 // not found / not executable
	}
	return strings.TrimSpace(string(out)), 0
}

// Run executes all checks and returns ordered results.
func (r *Runner) Run() []CheckResult {
	results := []CheckResult{
		r.checkConfig(),
		r.checkNotifiers(),
		r.checkDaemon(),
		r.checkEventStore(),
		r.checkSS(),
	}

	if r.cfg != nil && r.cfg.Firewall.Enabled {
		results = append(results, r.checkIPTables())
	} else {
		results = append(results, skip("Dependencies", "iptables", "Firewall watcher disabled"))
	}

	if r.cfg != nil && r.cfg.Docker.Enabled {
		results = append(results, r.checkDocker())
	} else {
		results = append(results, skip("Dependencies", "docker", "Docker watcher disabled"))
	}

	if r.cfg != nil && r.cfg.SecurityTools.Enabled {
		results = append(results, r.checkRKHunter())
		results = append(results, r.checkClamAV())
	} else {
		results = append(results, skip("Dependencies", "rkhunter", "Security tools disabled"))
		results = append(results, skip("Dependencies", "ClamAV", "Security tools disabled"))
	}

	if r.cfg != nil && r.cfg.Network.Enabled {
		results = append(results, r.checkIP())
	} else {
		results = append(results, skip("Dependencies", "ip", "Network watcher disabled"))
	}

	if r.cfg != nil && r.cfg.AutoUpdate.Enabled {
		results = append(results, r.checkAptGet())
	} else {
		results = append(results, skip("Dependencies", "apt-get", "Auto-update disabled"))
	}

	return results
}

// ── individual checks ────────────────────────────────────────────────────────

func (r *Runner) checkConfig() CheckResult {
	if r.cfg == nil {
		return CheckResult{
			Category: "Config", Name: "Config file",
			Status: StatusFail, Message: "Not loaded",
			Fix: "Run: sudo piguard setup",
		}
	}
	return CheckResult{Category: "Config", Name: "Config file", Status: StatusOK, Message: "Loaded successfully"}
}

func (r *Runner) checkNotifiers() CheckResult {
	if r.cfg == nil {
		return skip("Config", "Notifiers", "No config")
	}
	var enabled []string
	if r.cfg.Notifications.Telegram.Enabled {
		enabled = append(enabled, "Telegram")
	}
	if r.cfg.Notifications.Ntfy.Enabled {
		enabled = append(enabled, "ntfy")
	}
	if r.cfg.Notifications.Discord.Enabled {
		enabled = append(enabled, "Discord")
	}
	if r.cfg.Notifications.Webhook.Enabled {
		enabled = append(enabled, "Webhook")
	}
	if len(enabled) == 0 {
		return CheckResult{
			Category: "Config", Name: "Notifiers",
			Status: StatusFail, Message: "None enabled",
			Fix: "Enable at least one notifier in /etc/piguard/config.yaml",
		}
	}
	return CheckResult{Category: "Config", Name: "Notifiers", Status: StatusOK, Message: strings.Join(enabled, ", ")}
}

func (r *Runner) checkDaemon() CheckResult {
	out, code := r.execFn("systemctl", "is-active", "piguard")
	if code == -1 {
		return skip("Daemon", "Service", "systemd not available on this platform")
	}
	state := strings.TrimSpace(out)
	if state == "active" {
		return CheckResult{Category: "Daemon", Name: "Service", Status: StatusOK, Message: "piguard.service active"}
	}
	return CheckResult{
		Category: "Daemon", Name: "Service",
		Status: StatusFail, Message: fmt.Sprintf("piguard.service %s", state),
		Fix: "sudo systemctl enable --now piguard",
	}
}

func (r *Runner) checkEventStore() CheckResult {
	db, err := r.openDB(r.dbPath)
	if err != nil {
		return CheckResult{
			Category: "Daemon", Name: "Event store",
			Status: StatusFail, Message: "Cannot open database — has piguard run yet?",
			Fix: "sudo systemctl start piguard",
		}
	}
	defer db.Close()

	count, _ := db.GetEventCount(24)
	last, _ := db.GetLastAlertTime()

	msg := fmt.Sprintf("%d events today", count)
	if last != "" && last != "never" {
		msg += fmt.Sprintf(", last alert: %s", last)
	}
	return CheckResult{Category: "Daemon", Name: "Event store", Status: StatusOK, Message: msg}
}

func (r *Runner) checkSS() CheckResult {
	_, code := r.execFn("ss", "--version")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "ss",
			Status: StatusFail, Message: "Not found — port watcher will fail",
			Fix: "sudo apt install iproute2",
		}
	}
	return CheckResult{Category: "Dependencies", Name: "ss", Status: StatusOK, Message: "Available"}
}

func (r *Runner) checkIPTables() CheckResult {
	out, code := r.execFn("iptables", "-L", "INPUT", "-n")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "iptables",
			Status: StatusFail, Message: "Not found",
			Fix: "sudo apt install iptables",
		}
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "you must be root") {
		return CheckResult{
			Category: "Dependencies", Name: "iptables",
			Status: StatusWarn, Message: "Permission denied — firewall checks limited",
			Fix: "sudo chmod u+s $(which iptables)",
		}
	}
	return CheckResult{Category: "Dependencies", Name: "iptables", Status: StatusOK, Message: "Readable"}
}

func (r *Runner) checkDocker() CheckResult {
	out, code := r.execFn("docker", "info", "--format", "{{.Containers}}")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "docker",
			Status: StatusFail, Message: "Not found",
			Fix: "Install Docker: https://docs.docker.com/engine/install/",
		}
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "cannot connect") {
		return CheckResult{
			Category: "Dependencies", Name: "docker",
			Status: StatusWarn, Message: "Permission denied — docker watcher will fail",
			Fix: "sudo usermod -aG docker piguard && sudo systemctl restart piguard",
		}
	}
	if code != 0 {
		return CheckResult{
			Category: "Dependencies", Name: "docker",
			Status: StatusWarn, Message: "Docker daemon not running",
			Fix: "sudo systemctl start docker",
		}
	}
	return CheckResult{
		Category: "Dependencies", Name: "docker",
		Status: StatusOK, Message: fmt.Sprintf("Running (%s containers)", strings.TrimSpace(out)),
	}
}

func (r *Runner) checkRKHunter() CheckResult {
	_, code := r.execFn("rkhunter", "--version")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "rkhunter",
			Status: StatusWarn, Message: "Not installed — /scan will fail",
			Fix: "sudo apt install rkhunter",
		}
	}
	// If the log file exists but is not writable, /scan will error at runtime.
	logPath := "/var/log/rkhunter.log"
	if r.existsFn(logPath) && !r.writableFn(logPath) {
		return CheckResult{
			Category: "Dependencies", Name: "rkhunter",
			Status: StatusWarn, Message: "Log not writable — /scan will fail",
			Fix: "sudo chmod 666 /var/log/rkhunter.log",
		}
	}
	return CheckResult{Category: "Dependencies", Name: "rkhunter", Status: StatusOK, Message: "Installed"}
}

func (r *Runner) checkClamAV() CheckResult {
	_, code := r.execFn("clamscan", "--version")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "ClamAV",
			Status: StatusWarn, Message: "Not installed — /scan will fail",
			Fix: "sudo apt install clamav clamav-daemon",
		}
	}
	return CheckResult{Category: "Dependencies", Name: "ClamAV", Status: StatusOK, Message: "Installed"}
}

func (r *Runner) checkIP() CheckResult {
	_, code := r.execFn("ip", "neigh", "show")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "ip",
			Status: StatusFail, Message: "Not found — network watcher will fail",
			Fix: "sudo apt install iproute2",
		}
	}
	return CheckResult{Category: "Dependencies", Name: "ip", Status: StatusOK, Message: "Available"}
}

func (r *Runner) checkAptGet() CheckResult {
	_, code := r.execFn("apt-get", "--version")
	if code == -1 {
		return CheckResult{
			Category: "Dependencies", Name: "apt-get",
			Status: StatusFail, Message: "Not found — auto-update will fail",
			Fix: "Auto-update requires a Debian/Ubuntu-based system with apt-get",
		}
	}
	return CheckResult{Category: "Dependencies", Name: "apt-get", Status: StatusOK, Message: "Available"}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func skip(cat, name, msg string) CheckResult {
	return CheckResult{Category: cat, Name: name, Status: StatusSkip, Message: msg}
}

// ── renderers ────────────────────────────────────────────────────────────────

// RenderCLI formats results as a terminal-friendly report.
func RenderCLI(results []CheckResult) string {
	var b strings.Builder
	b.WriteString("🛡️  PiGuard Doctor\n")
	b.WriteString("─────────────────────────────────────\n\n")

	currentCat := ""
	for _, r := range results {
		if r.Category != currentCat {
			if currentCat != "" {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("  %s\n", r.Category))
			currentCat = r.Category
		}
		b.WriteString(fmt.Sprintf("  %s %-16s%s\n", r.Status.Emoji(), r.Name, r.Message))
		if r.Fix != "" && r.Status != StatusOK && r.Status != StatusSkip {
			b.WriteString(fmt.Sprintf("       ↳ %s\n", r.Fix))
		}
	}

	ok, warn, fail := tally(results)
	b.WriteString("\n─────────────────────────────────────\n")
	switch {
	case fail > 0:
		summary := fmt.Sprintf("  ❌ %d failed", fail)
		if warn > 0 {
			summary += fmt.Sprintf(", %d warning", warn)
		}
		b.WriteString(summary + "\n")
	case warn > 0:
		b.WriteString(fmt.Sprintf("  ⚠️  %d checks need attention\n", warn))
	default:
		b.WriteString(fmt.Sprintf("  ✅ All %d checks passed\n", ok))
	}
	return b.String()
}

// RenderTelegram formats results as Telegram HTML.
func RenderTelegram(results []CheckResult) string {
	var b strings.Builder
	b.WriteString("🛡️ <b>PiGuard Doctor</b>\n")

	currentCat := ""
	for _, r := range results {
		if r.Category != currentCat {
			b.WriteString(fmt.Sprintf("\n<b>%s</b>\n", r.Category))
			currentCat = r.Category
		}
		b.WriteString(fmt.Sprintf("%s %s — %s\n",
			r.Status.Emoji(), r.Name, html.EscapeString(r.Message)))
		if r.Fix != "" && r.Status != StatusOK && r.Status != StatusSkip {
			b.WriteString(fmt.Sprintf("   <code>%s</code>\n", html.EscapeString(r.Fix)))
		}
	}

	ok, warn, fail := tally(results)
	b.WriteString("\n")
	switch {
	case fail > 0:
		summary := fmt.Sprintf("❌ <b>%d failed", fail)
		if warn > 0 {
			summary += fmt.Sprintf(", %d warning", warn)
		}
		b.WriteString(summary + "</b>")
	case warn > 0:
		b.WriteString(fmt.Sprintf("⚠️ <b>%d checks need attention</b>", warn))
	default:
		b.WriteString(fmt.Sprintf("✅ <b>All %d checks passed</b>", ok))
	}
	return b.String()
}

func tally(results []CheckResult) (ok, warn, fail int) {
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
	}
	return
}
