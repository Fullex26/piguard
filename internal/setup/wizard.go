// Package setup implements the interactive PiGuard setup wizard.
package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

const DefaultEnvPath = "/etc/piguard/env"

// defaultConfigTemplate is written when no config file exists yet.
// Keep in sync with configs/default.yaml.
const defaultConfigTemplate = `# PiGuard Configuration
# https://github.com/Fullex26/piguard

# ── Notification channels (configure at least one) ──
notifications:
  telegram:
    enabled: false
    bot_token: "${PIGUARD_TELEGRAM_TOKEN}"
    chat_id: "${PIGUARD_TELEGRAM_CHAT_ID}"
    interactive: true  # Enable /commands in Telegram

  ntfy:
    enabled: false
    topic: "piguard-alerts"
    server: "https://ntfy.sh"
    token: ""

  discord:
    enabled: false
    webhook_url: "${PIGUARD_DISCORD_WEBHOOK}"

  webhook:
    enabled: false
    url: ""
    method: "POST"

# ── Port monitoring ──
ports:
  enabled: true
  ignore:
    - "127.0.0.1:*"
    - "::1:*"
  known: []
  cooldown: "15m"

# ── Firewall monitoring ──
firewall:
  enabled: true
  chains:
    - table: "filter"
      chain: "INPUT"
      expect_policy: "DROP"
    - table: "filter"
      chain: "DOCKER-USER"
      expect_rule: "DROP.*0.0.0.0/0"
  check_interval: "60s"

# ── System health ──
system:
  disk_threshold: 80
  memory_threshold: 90
  temperature_threshold: 75

# ── Alert behaviour ──
alerts:
  min_severity: "warning"
  daily_summary: "08:00"
  quiet_hours:
    start: "23:00"
    end: "07:00"

# ── Baseline ──
baseline:
  mode: "enforcing"
  learning_duration: "7d"

# ── Docker ──
docker:
  enabled: true

# ── File integrity monitoring ──
file_integrity:
  enabled: true
  cooldown: "5m"
  paths:
    - path: "/etc/passwd"
      description: "User accounts"
      severity: "critical"
    - path: "/etc/shadow"
      description: "Password hashes"
      severity: "critical"
    - path: "/etc/sudoers"
      description: "Sudo rules"
      severity: "critical"
    - path: "/etc/ssh/sshd_config"
      description: "SSH daemon config"
      severity: "critical"
    - path: "/etc/hosts"
      description: "Host resolution"
      severity: "warning"
    - path: "/etc/crontab"
      description: "System cron"
      severity: "warning"
    - path: "/etc/cron.d"
      description: "Cron job directory"
      severity: "warning"

# ── Security tool log monitoring (ClamAV / rkhunter) ──
security_tools:
  enabled: false
  clamav_log: "/var/log/clamav/clamav.log"
  rkhunter_log: "/var/log/rkhunter.log"
  poll_interval: "30s"
`

type wizardCreds struct {
	envVars    map[string]string // written to env file
	ntfyTopic  string            // written directly into config (not secret)
	ntfyServer string
	ntfyToken  string
}

// Run is the entry point for the interactive setup wizard.
func Run(configPath, envPath string) error {
	fmt.Println()
	fmt.Println("🛡️  PiGuard Setup")
	fmt.Println("─────────────────")
	fmt.Println()

	if err := ensureConfig(configPath); err != nil {
		return err
	}

	r := bufio.NewReader(os.Stdin)

	// ── Mode ────────────────────────────────────────────────────
	fmt.Println("  Choose setup mode:")
	fmt.Println("    [1] Simple   — guided setup with sensible defaults  (recommended)")
	fmt.Println("    [2] Advanced — configure every option")
	fmt.Println()
	fmt.Print("  Selection [1]: ")

	advanced := readLine(r) == "2"
	fmt.Println()

	// ── Notifier ─────────────────────────────────────────────────
	fmt.Println("  Choose a notification channel:")
	fmt.Println("    [1] Telegram")
	fmt.Println("    [2] Discord")
	fmt.Println("    [3] ntfy.sh  (push notifications, no account needed)")
	fmt.Println("    [4] Webhook")
	fmt.Println()
	fmt.Print("  Selection [1]: ")

	var notifier string
	switch readLine(r) {
	case "2":
		notifier = "discord"
	case "3":
		notifier = "ntfy"
	case "4":
		notifier = "webhook"
	default:
		notifier = "telegram"
	}
	fmt.Println()

	// ── Credentials ──────────────────────────────────────────────
	creds, err := collectCredentials(r, notifier)
	if err != nil {
		return err
	}

	// ── Write env file ───────────────────────────────────────────
	if len(creds.envVars) > 0 {
		if err := writeEnvFile(envPath, creds.envVars); err != nil {
			return fmt.Errorf("writing env file: %w", err)
		}
		// Set in current process so the test subprocess inherits them
		// (config.Load uses os.ExpandEnv which reads the process environment).
		for k, v := range creds.envVars {
			_ = os.Setenv(k, v)
		}
		fmt.Printf("  ✅ Credentials saved to %s\n", envPath)
	}

	// ── Update config ─────────────────────────────────────────────
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	updated := applyCredentials(string(configData), notifier, creds)
	if err := os.WriteFile(configPath, []byte(updated), 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	fmt.Printf("  ✅ Config updated: %s\n", configPath)
	fmt.Println()

	// ── Security tool monitoring ──────────────────────────────────
	updated, err = collectSecurityTools(r, updated)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(updated), 0600); err != nil {
		return fmt.Errorf("writing config (security tools): %w", err)
	}

	// ── Advanced settings ─────────────────────────────────────────
	if advanced {
		updated, err = collectAdvanced(r, updated)
		if err != nil {
			return err
		}
		if err := os.WriteFile(configPath, []byte(updated), 0600); err != nil {
			return fmt.Errorf("writing config (advanced): %w", err)
		}
		fmt.Println()
	}

	// ── Test notification ─────────────────────────────────────────
	fmt.Print("  Send a test notification? [Y/n]: ")
	if readBool(r, true) {
		fmt.Print("  Sending... ")
		if err := runTest(configPath); err != nil {
			fmt.Printf("\n  ⚠️  Test failed: %v\n", err)
			fmt.Println("  Check your credentials, then retry: sudo piguard test")
		} else {
			fmt.Println("✅")
		}
	}
	fmt.Println()

	// ── Start service ─────────────────────────────────────────────
	fmt.Print("  Enable and start piguard service? [Y/n]: ")
	if readBool(r, true) {
		if err := startService(); err != nil {
			fmt.Printf("  ⚠️  %v\n", err)
			fmt.Println("  Start manually: sudo systemctl enable --now piguard")
		} else {
			fmt.Println("  ✅ Service enabled and started!")
		}
	}

	fmt.Println()
	fmt.Println("✅ Setup complete!")
	fmt.Println("   Run 'sudo piguard status' to check security events.")
	fmt.Println()
	return nil
}

// ensureConfig creates the config file from the default template if absent.
func ensureConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	dir := path[:strings.LastIndexByte(path, '/')]
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0600); err != nil {
		return fmt.Errorf("creating default config: %w", err)
	}
	fmt.Printf("  Created default config: %s\n\n", path)
	return nil
}

// collectCredentials prompts for notifier-specific secrets.
func collectCredentials(r *bufio.Reader, notifier string) (wizardCreds, error) {
	c := wizardCreds{envVars: make(map[string]string)}

	switch notifier {
	case "telegram":
		fmt.Println("  Telegram")
		fmt.Println("  ──────────────────────────────────────────────────────────")
		fmt.Println("  1. Open Telegram and message @BotFather → /newbot")
		fmt.Println("  2. Get your Chat ID by messaging @userinfobot")
		fmt.Println()

		token, err := readMasked(r, "  Bot token:  ")
		if err != nil {
			return c, err
		}
		fmt.Print("  Chat ID:    ")
		chatID := readLine(r)
		c.envVars["PIGUARD_TELEGRAM_TOKEN"] = strings.TrimSpace(token)
		c.envVars["PIGUARD_TELEGRAM_CHAT_ID"] = strings.TrimSpace(chatID)

	case "discord":
		fmt.Println("  Discord")
		fmt.Println("  ──────────────────────────────────────────────────────────")
		fmt.Println("  Server Settings → Integrations → Webhooks → New Webhook")
		fmt.Println()

		u, err := readMasked(r, "  Webhook URL: ")
		if err != nil {
			return c, err
		}
		c.envVars["PIGUARD_DISCORD_WEBHOOK"] = strings.TrimSpace(u)

	case "ntfy":
		fmt.Println("  ntfy.sh")
		fmt.Println("  ──────────────────────────────────────────────────────────")
		fmt.Println("  Subscribe to your topic in the ntfy app to receive alerts.")
		fmt.Println()

		fmt.Print("  Topic name [piguard-alerts]: ")
		topic := strings.TrimSpace(readLine(r))
		if topic == "" {
			topic = "piguard-alerts"
		}
		fmt.Print("  Server     [https://ntfy.sh]: ")
		server := strings.TrimSpace(readLine(r))
		if server == "" {
			server = "https://ntfy.sh"
		}
		token, err := readMasked(r, "  Access token (optional, Enter to skip): ")
		if err != nil {
			return c, err
		}
		c.ntfyTopic = topic
		c.ntfyServer = server
		c.ntfyToken = strings.TrimSpace(token)

	case "webhook":
		fmt.Println("  Webhook")
		fmt.Println("  ──────────────────────────────────────────────────────────")
		fmt.Println("  PiGuard will POST JSON events to this URL.")
		fmt.Println()

		u, err := readMasked(r, "  URL: ")
		if err != nil {
			return c, err
		}
		c.envVars["PIGUARD_WEBHOOK_URL"] = strings.TrimSpace(u)
	}

	fmt.Println()
	return c, nil
}

// applyCredentials updates the config YAML for the selected notifier.
func applyCredentials(cfg, notifier string, c wizardCreds) string {
	cfg = setInBlock(cfg, notifier, "    enabled: false", "    enabled: true")

	switch notifier {
	case "ntfy":
		cfg = setInBlock(cfg, "ntfy", `    topic: "piguard-alerts"`, fmt.Sprintf(`    topic: "%s"`, c.ntfyTopic))
		cfg = setInBlock(cfg, "ntfy", `    server: "https://ntfy.sh"`, fmt.Sprintf(`    server: "%s"`, c.ntfyServer))
		if c.ntfyToken != "" {
			cfg = setInBlock(cfg, "ntfy", `    token: ""`, fmt.Sprintf(`    token: "%s"`, c.ntfyToken))
		}
	case "webhook":
		// Set the env-var placeholder so config.Load expands it at runtime.
		cfg = setInBlock(cfg, "webhook", `    url: ""`, `    url: "${PIGUARD_WEBHOOK_URL}"`)
	}

	return cfg
}

// setInBlock replaces old with replacement within the YAML block that begins
// with "  {notifier}:\n". The block ends at the first non-empty line whose
// indentation is less than 4 spaces (i.e. a sibling or parent key).
func setInBlock(cfg, notifier, old, replacement string) string {
	marker := "  " + notifier + ":\n"
	idx := strings.Index(cfg, marker)
	if idx == -1 {
		return cfg
	}

	after := cfg[idx+len(marker):]

	// Walk lines to find the end of this block.
	end := len(after)
	pos := 0
	for pos < len(after) {
		nl := strings.IndexByte(after[pos:], '\n')
		var line string
		if nl == -1 {
			// Last line with no trailing newline.
			end = len(after)
			break
		}
		line = after[pos : pos+nl]
		// Non-empty line with indent < 4 spaces signals end of block.
		if len(line) > 0 && !strings.HasPrefix(line, "    ") {
			end = pos
			break
		}
		pos += nl + 1
	}

	block := strings.Replace(after[:end], old, replacement, 1)
	return cfg[:idx+len(marker)] + block + after[end:]
}

// collectAdvanced prompts for optional advanced configuration options.
func collectAdvanced(r *bufio.Reader, cfg string) (string, error) {
	fmt.Println("  ── Advanced Settings ──────────────────────────────────────")
	fmt.Println("  (Press Enter to keep the default shown in brackets)")
	fmt.Println()

	fmt.Print("  Minimum alert severity [info/warning/critical] (default: warning): ")
	if v := strings.TrimSpace(readLine(r)); v != "" {
		cfg = strings.Replace(cfg, `  min_severity: "warning"`, fmt.Sprintf(`  min_severity: "%s"`, v), 1)
	}

	fmt.Print("  Daily summary time HH:MM (default: 08:00, empty to disable): ")
	if v := strings.TrimSpace(readLine(r)); v != "" {
		cfg = strings.Replace(cfg, `  daily_summary: "08:00"`, fmt.Sprintf(`  daily_summary: "%s"`, v), 1)
	}

	fmt.Print("  Quiet hours start HH:MM (default: 23:00): ")
	if v := strings.TrimSpace(readLine(r)); v != "" {
		cfg = strings.Replace(cfg, `    start: "23:00"`, fmt.Sprintf(`    start: "%s"`, v), 1)
	}

	fmt.Print("  Quiet hours end   HH:MM (default: 07:00): ")
	if v := strings.TrimSpace(readLine(r)); v != "" {
		cfg = strings.Replace(cfg, `    end: "07:00"`, fmt.Sprintf(`    end: "%s"`, v), 1)
	}

	fmt.Print("  Disk threshold %% (default: 80): ")
	if v := strings.TrimSpace(readLine(r)); v != "" && v != "80" {
		cfg = strings.Replace(cfg, "  disk_threshold: 80", "  disk_threshold: "+v, 1)
	}

	fmt.Print("  Memory threshold %% (default: 90): ")
	if v := strings.TrimSpace(readLine(r)); v != "" && v != "90" {
		cfg = strings.Replace(cfg, "  memory_threshold: 90", "  memory_threshold: "+v, 1)
	}

	fmt.Print("  CPU temperature threshold °C (default: 75): ")
	if v := strings.TrimSpace(readLine(r)); v != "" && v != "75" {
		cfg = strings.Replace(cfg, "  temperature_threshold: 75", "  temperature_threshold: "+v, 1)
	}

	fmt.Print("  Port change cooldown duration (default: 15m): ")
	if v := strings.TrimSpace(readLine(r)); v != "" && v != "15m" {
		cfg = strings.Replace(cfg, `  cooldown: "15m"`, fmt.Sprintf(`  cooldown: "%s"`, v), 1)
	}

	return cfg, nil
}

// collectSecurityTools asks whether to enable ClamAV and/or rkhunter log
// monitoring, auto-detecting whether each tool is installed.
func collectSecurityTools(r *bufio.Reader, cfg string) (string, error) {
	fmt.Println("  ── Security Tool Monitoring ───────────────────────────────")
	fmt.Println("  PiGuard can tail ClamAV and rkhunter logs and fire Critical")
	fmt.Println("  alerts the moment a scan finds malware or a rootkit warning.")
	fmt.Println("  (Install: sudo apt install clamav rkhunter)")
	fmt.Println()

	clamavFound := toolInstalled("/usr/bin/clamscan", "/usr/sbin/clamd", "/usr/bin/clamav")
	rkhunterFound := toolInstalled("/usr/bin/rkhunter", "/usr/local/bin/rkhunter")

	if clamavFound {
		fmt.Print("  ClamAV detected — enable log monitoring? [Y/n]: ")
	} else {
		fmt.Print("  ClamAV not found  — enable log monitoring? [y/N]: ")
	}
	enableSecTools := readBool(r, clamavFound)

	if rkhunterFound {
		fmt.Print("  rkhunter detected — enable log monitoring? [Y/n]: ")
	} else {
		fmt.Print("  rkhunter not found — enable log monitoring? [y/N]: ")
	}
	enableSecTools = enableSecTools || readBool(r, rkhunterFound)

	if enableSecTools {
		cfg = strings.Replace(cfg,
			"security_tools:\n  enabled: false",
			"security_tools:\n  enabled: true",
			1)
		fmt.Println("  ✅ Security tool log monitoring enabled")
	}

	fmt.Println()
	return cfg, nil
}

// toolInstalled returns true if any of the given binary paths exist on disk.
func toolInstalled(paths ...string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// writeEnvFile writes KEY=value pairs to path (one per line, mode 0600).
func writeEnvFile(path string, vars map[string]string) error {
	var sb strings.Builder
	for k, v := range vars {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(v)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0600)
}

// runTest invokes the current binary's "test" subcommand to verify notifiers.
// The child process inherits the parent's environment, so any os.Setenv calls
// made before this are visible to config.Load → os.ExpandEnv.
func runTest(configPath string) error {
	self, err := os.Executable()
	if err != nil {
		self = "piguard"
	}
	cmd := exec.Command(self, "--config", configPath, "test")
	// Route subprocess output through our stdout/stderr so errors are visible.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// startService enables and starts the piguard systemd service.
func startService() error {
	out, err := exec.Command("systemctl", "enable", "--now", "piguard").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// readLine reads one line from r, stripping the trailing newline.
func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// readMasked reads a secret without echoing characters when stdin is a TTY.
// Falls back to plain line reading for non-interactive contexts (pipes, CI).
func readMasked(r *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println() // restore cursor to new line
		if err != nil {
			return "", fmt.Errorf("reading secret: %w", err)
		}
		return string(b), nil
	}
	return readLine(r), nil
}

// readBool parses a y/n response; returns defaultVal on empty input.
func readBool(r *bufio.Reader, defaultVal bool) bool {
	line := strings.ToLower(strings.TrimSpace(readLine(r)))
	if line == "" {
		return defaultVal
	}
	return line == "y" || line == "yes"
}
