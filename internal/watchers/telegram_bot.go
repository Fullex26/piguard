package watchers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/analysers"
	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/store"
)

const telegramPollInterval = 2 * time.Second

// TelegramBotWatcher polls for incoming Telegram messages and handles commands
type TelegramBotWatcher struct {
	Base
	token    string
	chatID   string
	client   *http.Client
	offset   int
	labeller *analysers.PortLabeller
	store    *store.Store
}

func NewTelegramBotWatcher(cfg *config.Config, bus *eventbus.Bus, db *store.Store) *TelegramBotWatcher {
	return &TelegramBotWatcher{
		Base:     Base{Cfg: cfg, Bus: bus},
		token:    cfg.Notifications.Telegram.BotToken,
		chatID:   cfg.Notifications.Telegram.ChatID,
		client:   &http.Client{Timeout: 35 * time.Second},
		labeller: analysers.NewPortLabeller(),
		store:    db,
	}
}

func (w *TelegramBotWatcher) Name() string { return "telegram-bot" }

func (w *TelegramBotWatcher) Start(ctx context.Context) error {
	if w.token == "" || w.chatID == "" {
		slog.Info("telegram bot watcher disabled (no token/chat_id)")
		return nil
	}

	slog.Info("starting telegram bot command handler")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			w.poll(ctx)
		}
	}
}

func (w *TelegramBotWatcher) Stop() error { return nil }

// poll uses long polling to get updates from Telegram
func (w *TelegramBotWatcher) poll(ctx context.Context) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30&allowed_updates=[\"message\"]",
		w.token, w.offset)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return
	}

	resp, err := w.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return // context cancelled, shutting down
		}
		slog.Error("telegram poll failed", "error", err)
		time.Sleep(5 * time.Second)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int `json:"update_id"`
			Message  struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				Text string `json:"text"`
				From struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"from"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return
	}

	for _, update := range result.Result {
		w.offset = update.UpdateID + 1

		// Security: only respond to the configured chat ID
		chatIDInt, _ := strconv.ParseInt(w.chatID, 10, 64)
		if update.Message.Chat.ID != chatIDInt {
			slog.Warn("ignoring message from unauthorized chat",
				"chat_id", update.Message.Chat.ID,
				"username", update.Message.From.Username)
			continue
		}

		w.handleCommand(update.Message.Text)
	}
}

func (w *TelegramBotWatcher) handleCommand(text string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return // Ignore non-commands
	}

	// Split command and args
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])

	slog.Info("telegram command received", "command", cmd)

	var response string

	switch cmd {
	case "/start", "/help":
		response = w.cmdHelp()
	case "/status":
		response = w.cmdStatus()
	case "/ports":
		response = w.cmdPorts()
	case "/firewall", "/fw":
		response = w.cmdFirewall()
	case "/docker", "/containers":
		response = w.cmdDocker()
	case "/disk":
		response = w.cmdDisk()
	case "/temp", "/temperature":
		response = w.cmdTemp()
	case "/memory", "/mem", "/ram":
		response = w.cmdMemory()
	case "/uptime":
		response = w.cmdUptime()
	case "/events", "/logs":
		response = w.cmdEvents()
	case "/scan":
		response = w.cmdScan()
	case "/ip":
		response = w.cmdIP()
	case "/services":
		response = w.cmdServices()
	case "/reboot":
		response = w.cmdReboot(parts)
	default:
		response = fmt.Sprintf("Unknown command: %s\nSend /help for available commands.", cmd)
	}

	w.sendReply(response)
}

func (w *TelegramBotWatcher) sendReply(text string) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", w.token)

	data := url.Values{}
	data.Set("chat_id", w.chatID)
	data.Set("parse_mode", "HTML")
	data.Set("text", text)

	resp, err := http.PostForm(apiURL, data)
	if err != nil {
		slog.Error("telegram reply failed", "error", err)
		return
	}
	resp.Body.Close()
}

// ── Command implementations ──

func (w *TelegramBotWatcher) cmdHelp() string {
	return `🛡️ <b>PiGuard Commands</b>

<b>System</b>
/status — Full system overview
/disk — Storage usage
/memory — RAM usage
/temp — CPU temperature
/uptime — System uptime
/ip — Network addresses

<b>Security</b>
/ports — Listening ports with labels
/firewall — iptables rule check
/events — Recent security events
/scan — Trigger security scan

<b>Docker</b>
/docker — Container status
/services — Running services

<b>Danger zone</b>
/reboot CONFIRM — Reboot the Pi`
}

func (w *TelegramBotWatcher) cmdStatus() string {
	hostname, _ := os.Hostname()

	disk := w.getDiskStr()
	mem := w.getMemStr()
	temp := w.getTempStr()
	uptime := w.getUptimeStr()
	containers := w.getContainerSummary()
	ports := w.getPortCount()
	fw := w.getFirewallStatus()

	var lastAlert string
	if w.store != nil {
		lastAlert, _ = w.store.GetLastAlertTime()
	} else {
		lastAlert = "unknown"
	}

	return fmt.Sprintf(`🛡️ <b>PiGuard — %s</b>

<b>System</b>
  💾 Disk: %s
  🧠 RAM: %s
  🌡️ Temp: %s
  ⏱️ Uptime: %s

<b>Security</b>
  🔥 Firewall: %s
  🔌 Ports: %s
  🐳 Containers: %s
  ⚠️ Last alert: %s`,
		hostname, disk, mem, temp, uptime, fw, ports, containers, lastAlert)
}

func (w *TelegramBotWatcher) cmdPorts() string {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return "❌ Failed to read ports"
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "✅ No listening ports"
	}

	var b strings.Builder
	b.WriteString("🔌 <b>Listening Ports</b>\n\n")

	exposed := 0
	local := 0

	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		addr := fields[3]
		procName := "unknown"
		if len(fields) >= 6 {
			if idx := strings.Index(fields[5], "((\""); idx >= 0 {
				nameStr := fields[5][idx+3:]
				if end := strings.Index(nameStr, "\""); end > 0 {
					procName = nameStr[:end]
				}
			}
		}

		host, _, _ := strings.Cut(addr, ":")
		isExposed := host == "0.0.0.0" || host == "::" || host == "*"

		icon := "✅"
		if isExposed {
			icon = "⚠️"
			exposed++
		} else {
			local++
		}

		b.WriteString(fmt.Sprintf("%s <code>%s</code> → %s\n", icon, addr, procName))
	}

	b.WriteString(fmt.Sprintf("\n📊 %d local, %d exposed", local, exposed))
	return b.String()
}

func (w *TelegramBotWatcher) cmdFirewall() string {
	var b strings.Builder
	b.WriteString("🔥 <b>Firewall Status</b>\n\n")

	// INPUT policy
	out, err := exec.Command("iptables", "-L", "INPUT", "-n").Output()
	if err != nil {
		b.WriteString("❌ Cannot read INPUT chain (need root?)\n")
	} else {
		firstLine := strings.Split(string(out), "\n")[0]
		if strings.Contains(firstLine, "DROP") {
			b.WriteString("✅ INPUT policy: DROP\n")
		} else {
			b.WriteString("🔴 INPUT policy: NOT DROP — EXPOSED\n")
		}
	}

	// DOCKER-USER
	out, err = exec.Command("iptables", "-L", "DOCKER-USER", "-n").Output()
	if err != nil {
		b.WriteString("❌ Cannot read DOCKER-USER chain\n")
	} else {
		if strings.Contains(string(out), "DROP") {
			rules := strings.Count(string(out), "\n") - 2
			b.WriteString(fmt.Sprintf("✅ DOCKER-USER: intact (%d rules)\n", rules))
		} else {
			b.WriteString("🔴 DOCKER-USER: DROP rule MISSING\n")
		}
	}

	return b.String()
}

func (w *TelegramBotWatcher) cmdDocker() string {
	out, err := exec.Command("docker", "ps", "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}").Output()
	if err != nil {
		return "❌ Docker not available"
	}

	result := strings.TrimSpace(string(out))
	if result == "" || strings.Count(result, "\n") == 0 {
		return "🐳 No containers running"
	}

	// Parse into nicer format
	lines := strings.Split(result, "\n")
	var b strings.Builder
	b.WriteString("🐳 <b>Docker Containers</b>\n\n")

	for i, line := range lines {
		if i == 0 {
			continue // skip header
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		name := fields[0]
		// Determine health from status field
		status := strings.Join(fields[1:], " ")

		icon := "✅"
		if strings.Contains(status, "unhealthy") {
			icon = "🔴"
		} else if strings.Contains(status, "starting") {
			icon = "🟡"
		}

		b.WriteString(fmt.Sprintf("%s <b>%s</b>\n   %s\n", icon, name, status))
	}

	return b.String()
}

func (w *TelegramBotWatcher) cmdDisk() string {
	out, err := exec.Command("df", "-h", "/").Output()
	if err != nil {
		return "❌ Failed to read disk"
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "❌ No disk data"
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return "❌ Cannot parse disk data"
	}

	percent, _ := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
	bar := progressBar(percent)

	return fmt.Sprintf("💾 <b>Disk Usage</b>\n\n%s %s\n\nTotal: %s | Used: %s | Free: %s",
		bar, fields[4], fields[1], fields[2], fields[3])
}

func (w *TelegramBotWatcher) cmdTemp() string {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return "🌡️ Temperature sensor not available"
	}

	millideg, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	temp := millideg / 1000.0

	icon := "✅"
	if temp > 70 {
		icon = "🔴"
	} else if temp > 60 {
		icon = "🟡"
	}

	return fmt.Sprintf("🌡️ <b>CPU Temperature</b>\n\n%s %.1f°C", icon, temp)
}

func (w *TelegramBotWatcher) cmdMemory() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "❌ Failed to read memory"
	}

	var total, available, buffers, cached int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		case "Buffers:":
			buffers = val
		case "Cached:":
			cached = val
		}
	}

	if total == 0 {
		return "❌ Cannot read memory info"
	}

	used := total - available
	percent := int((used * 100) / total)
	bar := progressBar(percent)

	return fmt.Sprintf(`🧠 <b>Memory Usage</b>

%s %d%%

Total: %s | Used: %s | Available: %s
Buffers: %s | Cached: %s`,
		bar, percent,
		formatKB(total), formatKB(used), formatKB(available),
		formatKB(buffers), formatKB(cached))
}

func (w *TelegramBotWatcher) cmdUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "❌ Failed to read uptime"
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return "❌ Cannot parse uptime"
	}

	seconds, _ := strconv.ParseFloat(fields[0], 64)
	days := int(seconds) / 86400
	hours := (int(seconds) % 86400) / 3600
	mins := (int(seconds) % 3600) / 60

	return fmt.Sprintf("⏱️ <b>Uptime</b>\n\n%d days, %d hours, %d minutes", days, hours, mins)
}

func (w *TelegramBotWatcher) cmdEvents() string {
	if w.store == nil {
		return "❌ Event store not available"
	}

	events, err := w.store.GetRecentEvents(24)
	if err != nil {
		return "❌ Failed to read events"
	}

	if len(events) == 0 {
		return "✅ No security events in last 24 hours"
	}

	var b strings.Builder
	b.WriteString("📋 <b>Recent Events (24h)</b>\n\n")

	limit := 15
	if len(events) < limit {
		limit = len(events)
	}

	for _, e := range events[:limit] {
		b.WriteString(fmt.Sprintf("%s <code>%s</code> %s\n",
			e.Severity.Emoji(),
			e.Timestamp.Format("15:04"),
			e.Message,
		))
	}

	if len(events) > limit {
		b.WriteString(fmt.Sprintf("\n... and %d more", len(events)-limit))
	}

	return b.String()
}

func (w *TelegramBotWatcher) cmdScan() string {
	w.sendReply("🔍 Starting security scan... this may take a few minutes.")

	var b strings.Builder
	b.WriteString("🔍 <b>Security Scan Results</b>\n\n")

	// rkhunter
	out, err := exec.Command("rkhunter", "--check", "--skip-keypress", "--report-warnings-only").CombinedOutput()
	if err != nil {
		rkhunterOut := strings.TrimSpace(string(out))
		if rkhunterOut != "" {
			b.WriteString(fmt.Sprintf("⚠️ <b>rkhunter:</b>\n<code>%s</code>\n\n", truncate(rkhunterOut, 500)))
		} else {
			b.WriteString("✅ <b>rkhunter:</b> No warnings\n\n")
		}
	} else {
		b.WriteString("✅ <b>rkhunter:</b> No warnings\n\n")
	}

	// Quick ClamAV scan of key dirs
	out, err = exec.Command("clamscan", "-r", "--quiet", "--infected", "/home", "/tmp", "/var/tmp").CombinedOutput()
	clamOut := strings.TrimSpace(string(out))
	if clamOut != "" {
		b.WriteString(fmt.Sprintf("⚠️ <b>ClamAV:</b>\n<code>%s</code>\n", truncate(clamOut, 500)))
	} else {
		b.WriteString("✅ <b>ClamAV:</b> No threats found\n")
	}

	return b.String()
}

func (w *TelegramBotWatcher) cmdIP() string {
	hostname, _ := os.Hostname()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🌐 <b>Network — %s</b>\n\n", hostname))

	// LAN IP
	out, _ := exec.Command("hostname", "-I").Output()
	ips := strings.Fields(strings.TrimSpace(string(out)))
	for _, ip := range ips {
		if strings.Contains(ip, ":") {
			continue // skip IPv6 for readability
		}
		label := "LAN"
		if strings.HasPrefix(ip, "100.") {
			label = "Tailscale"
		} else if strings.HasPrefix(ip, "172.") || strings.HasPrefix(ip, "10.") {
			label = "Docker"
		}
		b.WriteString(fmt.Sprintf("  %s: <code>%s</code>\n", label, ip))
	}

	return b.String()
}

func (w *TelegramBotWatcher) cmdServices() string {
	out, err := exec.Command("systemctl", "list-units", "--type=service", "--state=running", "--no-pager", "--no-legend").Output()
	if err != nil {
		return "❌ Failed to list services"
	}

	var b strings.Builder
	b.WriteString("⚙️ <b>Running Services</b>\n\n")

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ".service")
		// Filter out noise
		if strings.HasPrefix(name, "sys-") || strings.HasPrefix(name, "user@") ||
			strings.HasPrefix(name, "systemd-") || name == "dbus" ||
			strings.HasPrefix(name, "modprobe@") || strings.HasPrefix(name, "getty@") {
			continue
		}
		b.WriteString(fmt.Sprintf("  ✅ %s\n", name))
		count++
	}

	b.WriteString(fmt.Sprintf("\n📊 %d services running", count))
	return b.String()
}

func (w *TelegramBotWatcher) cmdReboot(parts []string) string {
	if len(parts) < 2 || strings.ToUpper(parts[1]) != "CONFIRM" {
		return "⚠️ <b>Reboot requires confirmation</b>\n\nSend: /reboot CONFIRM"
	}

	w.sendReply("🔄 Rebooting in 5 seconds...")

	go func() {
		time.Sleep(5 * time.Second)
		exec.Command("reboot").Run()
	}()

	return ""
}

// ── Helper functions ──

func (w *TelegramBotWatcher) getDiskStr() string {
	out, err := exec.Command("df", "-h", "/").Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "unknown"
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return "unknown"
	}
	return fmt.Sprintf("%s / %s (%s)", fields[2], fields[1], fields[4])
}

func (w *TelegramBotWatcher) getMemStr() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "unknown"
	}
	var total, available int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		}
	}
	if total == 0 {
		return "unknown"
	}
	used := total - available
	pct := (used * 100) / total
	return fmt.Sprintf("%s / %s (%d%%)", formatKB(used), formatKB(total), pct)
}

func (w *TelegramBotWatcher) getTempStr() string {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return "N/A"
	}
	millideg, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	return fmt.Sprintf("%.1f°C", millideg/1000.0)
}

func (w *TelegramBotWatcher) getUptimeStr() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	days := int(seconds) / 86400
	hours := (int(seconds) % 86400) / 3600
	return fmt.Sprintf("%dd %dh", days, hours)
}

func (w *TelegramBotWatcher) getContainerSummary() string {
	out, err := exec.Command("docker", "ps", "-q").Output()
	if err != nil {
		return "N/A"
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			count++
		}
	}
	return fmt.Sprintf("%d running", count)
}

func (w *TelegramBotWatcher) getPortCount() string {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := len(lines) - 1
	if count < 0 {
		count = 0
	}
	return fmt.Sprintf("%d listening", count)
}

func (w *TelegramBotWatcher) getFirewallStatus() string {
	out, _ := exec.Command("iptables", "-L", "INPUT", "-n").Output()
	if strings.Contains(string(out), "DROP") {
		return "✅ intact"
	}
	return "🔴 CHECK REQUIRED"
}

// progressBar creates a visual bar like [████████░░] 80%
func progressBar(percent int) string {
	filled := percent / 10
	empty := 10 - filled
	if filled > 10 {
		filled = 10
		empty = 0
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

// formatKB converts kB to human-readable
func formatKB(kb int64) string {
	if kb > 1048576 {
		return fmt.Sprintf("%.1f GB", float64(kb)/1048576)
	}
	if kb > 1024 {
		return fmt.Sprintf("%.0f MB", float64(kb)/1024)
	}
	return fmt.Sprintf("%d kB", kb)
}

// truncate limits string length
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
