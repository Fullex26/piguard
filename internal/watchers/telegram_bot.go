package watchers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
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
	"github.com/Fullex26/piguard/internal/doctor"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/internal/notifiers"
	"github.com/Fullex26/piguard/internal/store"
)

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
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30&allowed_updates=[\"message\",\"callback_query\"]",
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
			Message  *struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				Text string `json:"text"`
				From struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"from"`
			} `json:"message"`
			CallbackQuery *struct {
				ID   string `json:"id"`
				From struct {
					ID       int64  `json:"id"`
					Username string `json:"username"`
				} `json:"from"`
				Message struct {
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
				} `json:"message"`
				Data string `json:"data"`
			} `json:"callback_query"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return
	}

	chatIDInt, _ := strconv.ParseInt(w.chatID, 10, 64)

	for _, update := range result.Result {
		w.offset = update.UpdateID + 1

		// Handle callback queries (inline button taps)
		if update.CallbackQuery != nil {
			if update.CallbackQuery.Message.Chat.ID != chatIDInt {
				continue
			}
			w.handleCallback(update.CallbackQuery.ID, update.CallbackQuery.Data)
			continue
		}

		// Handle regular messages
		if update.Message == nil {
			continue
		}
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
		response = w.cmdDockerRouter(parts)
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
	case "/doctor":
		response = w.cmdDoctor()
	case "/updates":
		response = w.cmdUpdates()
	case "/update":
		response = w.cmdUpdate(parts)
	case "/storage":
		response = w.cmdStorageRouter(parts)
	case "/report":
		response = w.cmdReport()
	case "/reboot":
		response = w.cmdReboot(parts)
	default:
		response = fmt.Sprintf("Unknown command: %s\nSend /help for available commands.", cmd)
	}

	if response != "" {
		w.sendReply(response)
	}
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

// InlineButton represents a Telegram inline keyboard button.
type InlineButton struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

// sendReplyWithKeyboard sends a message with an inline keyboard.
func (w *TelegramBotWatcher) sendReplyWithKeyboard(text string, buttons [][]InlineButton) {
	keyboard := struct {
		InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
	}{InlineKeyboard: buttons}

	markup, _ := json.Marshal(keyboard)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", w.token)

	data := url.Values{}
	data.Set("chat_id", w.chatID)
	data.Set("parse_mode", "HTML")
	data.Set("text", text)
	data.Set("reply_markup", string(markup))

	resp, err := http.PostForm(apiURL, data)
	if err != nil {
		slog.Error("telegram reply with keyboard failed", "error", err)
		return
	}
	resp.Body.Close()
}

// answerCallbackQuery acknowledges a callback query (removes the loading spinner).
func (w *TelegramBotWatcher) answerCallbackQuery(callbackID string) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", w.token)
	data := url.Values{}
	data.Set("callback_query_id", callbackID)
	resp, err := http.PostForm(apiURL, data)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// handleCallback routes inline button presses to the appropriate command.
func (w *TelegramBotWatcher) handleCallback(callbackID, data string) {
	w.answerCallbackQuery(callbackID)

	slog.Info("telegram callback received", "data", data)

	var response string

	switch {
	case data == "reboot:confirm":
		response = w.cmdReboot([]string{"/reboot", "CONFIRM"})
	case data == "update:confirm":
		response = w.cmdUpdate([]string{"/update", "CONFIRM"})
	case data == "docker:prune":
		response = w.cmdDockerPrune([]string{"CONFIRM"})
	case strings.HasPrefix(data, "docker:rm:"):
		name := strings.TrimPrefix(data, "docker:rm:")
		response = w.cmdDockerRemove([]string{name, "CONFIRM"})
	case data == "storage:images":
		response = w.cmdStorageImages([]string{"CONFIRM"})
	case data == "storage:volumes":
		response = w.cmdStorageVolumes([]string{"CONFIRM"})
	case data == "storage:apt":
		response = w.cmdStorageApt([]string{"CONFIRM"})
	case data == "storage:all":
		response = w.cmdStorageAll([]string{"CONFIRM"})
	default:
		response = fmt.Sprintf("Unknown action: %s", data)
	}

	if response != "" {
		w.sendReply(response)
	}
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
/docker stop &lt;name&gt; — Stop a container
/docker restart &lt;name&gt; — Restart a container
/docker fix &lt;name&gt; — Restart unhealthy/exited container
/docker logs &lt;name&gt; — Show last 20 log lines
/docker remove &lt;name&gt; CONFIRM — Force-remove a container
/docker prune CONFIRM — Remove all stopped containers
/services — Running services

<b>Storage</b>
/storage — Disk + Docker space report
/storage images CONFIRM — Prune unused Docker images
/storage volumes CONFIRM — Prune unused Docker volumes
/storage apt CONFIRM — Clean apt package cache
/storage all CONFIRM — Run all pruning operations

<b>Updates</b>
/updates — Check available package upgrades
/update CONFIRM — Run apt upgrade now

<b>Reports</b>
/report — On-demand weekly trend report

<b>Diagnostics</b>
/doctor — Check PiGuard installation health

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

// cmdDockerRouter dispatches /docker subcommands. With no subcommand it falls
// back to the container-list view so existing behaviour is preserved.
func (w *TelegramBotWatcher) cmdDockerRouter(parts []string) string {
	if len(parts) < 2 {
		return w.cmdDocker()
	}
	args := parts[2:] // everything after the subcommand
	switch strings.ToLower(parts[1]) {
	case "stop":
		return w.cmdDockerStop(args)
	case "restart":
		return w.cmdDockerRestart(args)
	case "remove", "rm":
		return w.cmdDockerRemove(args)
	case "fix":
		return w.cmdDockerFix(args)
	case "logs":
		return w.cmdDockerLogs(args)
	case "prune":
		return w.cmdDockerPrune(args)
	default:
		return w.cmdDocker() + "\n\n<i>Usage: /docker [stop|restart|remove|fix|logs|prune] &lt;name&gt;</i>"
	}
}

func (w *TelegramBotWatcher) cmdDockerStop(args []string) string {
	if len(args) == 0 {
		return "Usage: /docker stop &lt;name&gt;"
	}
	name := args[0]
	out, err := exec.Command("docker", "stop", name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Failed to stop <b>%s</b>: %s",
			html.EscapeString(name), html.EscapeString(strings.TrimSpace(string(out))))
	}
	return fmt.Sprintf("⏹️ Container <b>%s</b> stopped.", html.EscapeString(name))
}

func (w *TelegramBotWatcher) cmdDockerRestart(args []string) string {
	if len(args) == 0 {
		return "Usage: /docker restart &lt;name&gt;"
	}
	name := args[0]
	out, err := exec.Command("docker", "restart", name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Failed to restart <b>%s</b>: %s",
			html.EscapeString(name), html.EscapeString(strings.TrimSpace(string(out))))
	}
	return fmt.Sprintf("🔄 Container <b>%s</b> restarted.", html.EscapeString(name))
}

func (w *TelegramBotWatcher) cmdDockerRemove(args []string) string {
	if len(args) == 0 {
		return "Usage: /docker remove &lt;name&gt; CONFIRM"
	}
	name := args[0]
	safeName := html.EscapeString(name)
	if len(args) < 2 || strings.ToUpper(args[len(args)-1]) != "CONFIRM" {
		w.sendReplyWithKeyboard(
			fmt.Sprintf("⚠️ This will force-remove container <b>%s</b>.", safeName),
			[][]InlineButton{{{Text: "🗑️ Remove " + name, Data: "docker:rm:" + name}}})
		return ""
	}
	out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Failed to remove <b>%s</b>: %s",
			safeName, html.EscapeString(strings.TrimSpace(string(out))))
	}
	return fmt.Sprintf("🗑️ Container <b>%s</b> removed.", safeName)
}

// cmdDockerFix is a UX alias for restart, targeted at unhealthy/exited containers.
func (w *TelegramBotWatcher) cmdDockerFix(args []string) string {
	if len(args) == 0 {
		return "Usage: /docker fix &lt;name&gt;"
	}
	name := args[0]
	out, err := exec.Command("docker", "restart", name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Failed to restart <b>%s</b>: %s",
			html.EscapeString(name), html.EscapeString(strings.TrimSpace(string(out))))
	}
	return fmt.Sprintf("🔧 Container <b>%s</b> restarted (fix applied).\nDockerWatcher will confirm recovery within 10s.", html.EscapeString(name))
}

func (w *TelegramBotWatcher) cmdDockerLogs(args []string) string {
	if len(args) == 0 {
		return "Usage: /docker logs &lt;name&gt;"
	}
	name := args[0]
	out, err := exec.Command("docker", "logs", "--tail", "20", name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Failed to get logs for <b>%s</b>: %s",
			html.EscapeString(name), html.EscapeString(strings.TrimSpace(string(out))))
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return fmt.Sprintf("📋 No log output for <b>%s</b>", html.EscapeString(name))
	}
	return fmt.Sprintf("📋 <b>Logs — %s</b> (last 20 lines)\n\n<code>%s</code>",
		html.EscapeString(name), truncate(html.EscapeString(result), 3000))
}

func (w *TelegramBotWatcher) cmdDockerPrune(args []string) string {
	if len(args) == 0 || strings.ToUpper(args[len(args)-1]) != "CONFIRM" {
		w.sendReplyWithKeyboard(
			"⚠️ <b>Docker system prune</b> removes all stopped containers, unused networks, dangling images, and build cache.",
			[][]InlineButton{{{Text: "🧹 Prune Docker", Data: "docker:prune"}}})
		return ""
	}
	w.sendReply("🧹 Running docker system prune...")
	out, err := exec.Command("docker", "system", "prune", "-f").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Prune failed: %s", truncate(html.EscapeString(strings.TrimSpace(string(out))), 500))
	}
	return fmt.Sprintf("🧹 <b>Docker pruned:</b>\n<code>%s</code>",
		truncate(html.EscapeString(strings.TrimSpace(string(out))), 800))
}

// cmdStorageRouter dispatches /storage subcommands.
// With no subcommand it shows the disk + Docker usage report.
func (w *TelegramBotWatcher) cmdStorageRouter(parts []string) string {
	if len(parts) < 2 {
		return w.cmdStorageReport()
	}
	args := parts[2:]
	switch strings.ToLower(parts[1]) {
	case "images":
		return w.cmdStorageImages(args)
	case "volumes":
		return w.cmdStorageVolumes(args)
	case "apt":
		return w.cmdStorageApt(args)
	case "all":
		return w.cmdStorageAll(args)
	default:
		return w.cmdStorageReport() + "\n\n<i>Usage: /storage [images|volumes|apt|all] CONFIRM</i>"
	}
}

func (w *TelegramBotWatcher) cmdStorageReport() string {
	var b strings.Builder
	b.WriteString("💾 <b>Storage Report</b>\n\n")

	// Root filesystem
	out, err := exec.Command("df", "-h", "/").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 5 {
				percent, _ := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
				bar := progressBar(percent)
				b.WriteString(fmt.Sprintf("<b>Root (/)</b>\n%s %s used of %s (free: %s)\n\n",
					bar, fields[2], fields[1], fields[3]))
			}
		}
	}

	// Docker space breakdown
	out, err = exec.Command("docker", "system", "df").Output()
	if err != nil {
		b.WriteString("🐳 Docker: not available\n")
	} else {
		b.WriteString("<b>Docker layers</b>\n<code>")
		b.WriteString(html.EscapeString(strings.TrimSpace(string(out))))
		b.WriteString("</code>\n")
	}

	b.WriteString("\n<i>To reclaim space: /storage images|volumes|apt|all CONFIRM</i>")
	return b.String()
}

func (w *TelegramBotWatcher) cmdStorageImages(args []string) string {
	if len(args) == 0 || strings.ToUpper(args[len(args)-1]) != "CONFIRM" {
		w.sendReplyWithKeyboard(
			"⚠️ <b>Prune unused Docker images</b> — removes all images not referenced by a container.",
			[][]InlineButton{{{Text: "🧹 Prune Images", Data: "storage:images"}}})
		return ""
	}
	w.sendReply("🧹 Pruning Docker images...")
	out, err := exec.Command("docker", "image", "prune", "-af").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Image prune failed: %s", truncate(html.EscapeString(strings.TrimSpace(string(out))), 500))
	}
	return fmt.Sprintf("🧹 <b>Images pruned:</b>\n<code>%s</code>",
		truncate(html.EscapeString(strings.TrimSpace(string(out))), 800))
}

func (w *TelegramBotWatcher) cmdStorageVolumes(args []string) string {
	if len(args) == 0 || strings.ToUpper(args[len(args)-1]) != "CONFIRM" {
		w.sendReplyWithKeyboard(
			"⚠️ <b>Prune unused Docker volumes</b> — removes volumes not attached to any container.",
			[][]InlineButton{{{Text: "🧹 Prune Volumes", Data: "storage:volumes"}}})
		return ""
	}
	w.sendReply("🧹 Pruning Docker volumes...")
	out, err := exec.Command("docker", "volume", "prune", "-f").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ Volume prune failed: %s", truncate(html.EscapeString(strings.TrimSpace(string(out))), 500))
	}
	return fmt.Sprintf("🧹 <b>Volumes pruned:</b>\n<code>%s</code>",
		truncate(html.EscapeString(strings.TrimSpace(string(out))), 800))
}

func (w *TelegramBotWatcher) cmdStorageApt(args []string) string {
	if len(args) == 0 || strings.ToUpper(args[len(args)-1]) != "CONFIRM" {
		w.sendReplyWithKeyboard(
			"⚠️ <b>Clean apt cache</b> — runs <code>apt-get clean &amp;&amp; apt-get autoremove -y</code>.",
			[][]InlineButton{{{Text: "🧹 Clean apt", Data: "storage:apt"}}})
		return ""
	}
	w.sendReply("🧹 Cleaning apt cache...")
	// apt-get clean never fails; apt-get autoremove may exit non-zero on warnings
	cleanOut, _ := exec.Command("apt-get", "clean").CombinedOutput()
	removeOut, removeErr := exec.Command("apt-get", "autoremove", "-y").CombinedOutput()

	var b strings.Builder
	b.WriteString("🧹 <b>apt cache cleaned</b>\n")
	if len(strings.TrimSpace(string(cleanOut))) > 0 {
		b.WriteString(fmt.Sprintf("<code>%s</code>\n", truncate(html.EscapeString(strings.TrimSpace(string(cleanOut))), 300)))
	}
	if removeErr != nil {
		b.WriteString(fmt.Sprintf("⚠️ autoremove: <code>%s</code>", truncate(html.EscapeString(strings.TrimSpace(string(removeOut))), 400)))
	} else {
		b.WriteString(fmt.Sprintf("✅ autoremove: <code>%s</code>", truncate(html.EscapeString(strings.TrimSpace(string(removeOut))), 400)))
	}
	return b.String()
}

func (w *TelegramBotWatcher) cmdStorageAll(args []string) string {
	if len(args) == 0 || strings.ToUpper(args[len(args)-1]) != "CONFIRM" {
		w.sendReplyWithKeyboard(
			"⚠️ <b>Full storage cleanup</b> — prunes Docker images, volumes, and apt cache.",
			[][]InlineButton{{{Text: "🧹 Full Cleanup", Data: "storage:all"}}})
		return ""
	}
	w.sendReply("🧹 Running full storage cleanup...")

	var b strings.Builder
	b.WriteString("🧹 <b>Full storage cleanup</b>\n\n")

	// Images
	imgOut, imgErr := exec.Command("docker", "image", "prune", "-af").CombinedOutput()
	if imgErr != nil {
		b.WriteString(fmt.Sprintf("❌ Images: %s\n", truncate(html.EscapeString(strings.TrimSpace(string(imgOut))), 200)))
	} else {
		// Extract the "Total reclaimed space" line if present
		reclaimed := extractReclaimedLine(string(imgOut))
		b.WriteString(fmt.Sprintf("✅ Images pruned%s\n", reclaimed))
	}

	// Volumes
	volOut, volErr := exec.Command("docker", "volume", "prune", "-f").CombinedOutput()
	if volErr != nil {
		b.WriteString(fmt.Sprintf("❌ Volumes: %s\n", truncate(html.EscapeString(strings.TrimSpace(string(volOut))), 200)))
	} else {
		reclaimed := extractReclaimedLine(string(volOut))
		b.WriteString(fmt.Sprintf("✅ Volumes pruned%s\n", reclaimed))
	}

	// apt
	exec.Command("apt-get", "clean").Run() //nolint:errcheck
	removeOut, removeErr := exec.Command("apt-get", "autoremove", "-y").CombinedOutput()
	if removeErr != nil {
		b.WriteString(fmt.Sprintf("⚠️ apt autoremove: %s\n", truncate(html.EscapeString(strings.TrimSpace(string(removeOut))), 200)))
	} else {
		b.WriteString("✅ apt cache cleaned\n")
	}

	return b.String()
}

// extractReclaimedLine returns " — <Total reclaimed space: X>" if found in docker output.
func extractReclaimedLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Total reclaimed space") {
			return " — " + strings.TrimSpace(line)
		}
	}
	return ""
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

	// rkhunter: exit 0 = clean, exit 1 = warnings found, exit 2+ = tool error
	out, err := exec.Command("rkhunter", "--check", "--skip-keypress", "--report-warnings-only").CombinedOutput()
	rkhunterOut := strings.TrimSpace(string(out))
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Genuine security warnings
			if rkhunterOut != "" {
				b.WriteString(fmt.Sprintf("⚠️ <b>rkhunter:</b>\n<code>%s</code>\n\n", truncate(html.EscapeString(rkhunterOut), 500)))
			} else {
				b.WriteString("⚠️ <b>rkhunter:</b> Warnings detected (check log)\n\n")
			}
		} else if strings.Contains(rkhunterOut, "not writable") {
			// Log file permissions issue — not a security finding, surface the fix.
			b.WriteString("❌ <b>rkhunter:</b> Log file not writable\n" +
				"Fix: <code>sudo chmod 666 /var/log/rkhunter.log</code>\n\n")
		} else {
			// Tool error (not installed, config issue, etc.) — not a security finding
			msg := rkhunterOut
			if msg == "" {
				msg = err.Error()
			}
			b.WriteString(fmt.Sprintf("❌ <b>rkhunter:</b> scan error\n<code>%s</code>\n\n", truncate(html.EscapeString(msg), 300)))
		}
	} else {
		b.WriteString("✅ <b>rkhunter:</b> No warnings\n\n")
	}

	// ClamAV: exit 0 = clean, exit 1 = infected files found, exit 2+ = tool error
	out, err = exec.Command("clamscan", "-r", "--quiet", "--infected", "/home", "/tmp", "/var/tmp").CombinedOutput()
	clamOut := strings.TrimSpace(string(out))
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Actual infections found — clamscan --infected only prints infected paths
			b.WriteString(fmt.Sprintf("⚠️ <b>ClamAV:</b>\n<code>%s</code>\n", truncate(html.EscapeString(clamOut), 500)))
		} else {
			// Tool error (temp dir permissions, library issue, etc.) — not a security finding
			msg := clamOut
			if msg == "" {
				msg = err.Error()
			}
			b.WriteString(fmt.Sprintf("❌ <b>ClamAV:</b> scan error\n<code>%s</code>\n", truncate(html.EscapeString(msg), 300)))
		}
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

	if section := dockerContainerURLs(); section != "" {
		b.WriteString("\n\n")
		b.WriteString(section)
	}

	return b.String()
}

// dockerContainerURLs returns a formatted Docker section listing running
// containers with their host port bindings as local access URLs.
// Returns "" when Docker is unavailable or no containers are running.
func dockerContainerURLs() string {
	out, err := exec.Command("docker", "ps", "--format", "{{json .}}").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return ""
	}

	localIP := getLocalIP()

	type entry struct {
		name string
		urls []string
	}
	var entries []entry

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c containerState
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		ports := parseHostPorts(c.Ports)
		var urls []string
		for _, p := range ports {
			urls = append(urls, fmt.Sprintf(":%s → http://%s:%s", p, localIP, p))
		}
		entries = append(entries, entry{name: c.Names, urls: urls})
	}

	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("🐳 <b>Docker Containers</b>\n\n")
	for _, e := range entries {
		if len(e.urls) == 0 {
			b.WriteString(fmt.Sprintf("  🐳 %s\n", e.name))
		} else {
			for _, u := range e.urls {
				b.WriteString(fmt.Sprintf("  🐳 %s  %s\n", e.name, u))
			}
		}
	}
	return b.String()
}

// parseHostPorts extracts unique host-side ports from a Docker Ports string.
// Input example: "0.0.0.0:8080->80/tcp, :::443->443/tcp"
// Returns: ["8080", "443"]
func parseHostPorts(ports string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, segment := range strings.Split(ports, ",") {
		segment = strings.TrimSpace(segment)
		arrowIdx := strings.Index(segment, "->")
		if arrowIdx < 0 {
			continue
		}
		hostPart := segment[:arrowIdx]
		colonIdx := strings.LastIndex(hostPart, ":")
		if colonIdx < 0 {
			continue
		}
		port := hostPart[colonIdx+1:]
		if port == "" {
			continue
		}
		if _, dup := seen[port]; dup {
			continue
		}
		seen[port] = struct{}{}
		result = append(result, port)
	}
	return result
}

// getLocalIP returns the first non-IPv6 address from `hostname -I`, falling
// back to "localhost" if the command fails or produces no usable output.
func getLocalIP() string {
	out, err := exec.Command("hostname", "-I").Output()
	if err != nil {
		return "localhost"
	}
	for _, field := range strings.Fields(string(out)) {
		if !strings.Contains(field, ":") {
			return field
		}
	}
	return "localhost"
}

func (w *TelegramBotWatcher) cmdReport() string {
	if w.store == nil {
		return "❌ Event store not available"
	}

	hostname, _ := os.Hostname()
	thisWeek, err := w.store.GetEventCountByType(7)
	if err != nil {
		return "❌ Failed to query events"
	}
	lastTwoWeeks, _ := w.store.GetEventCountByType(14)

	// Subtract this week from the 14-day total to get last week only
	lastWeekOnly := make(map[string]int)
	for k, v := range lastTwoWeeks {
		lastWeekOnly[k] = v - thisWeek[k]
	}

	totalThis := 0
	for _, v := range thisWeek {
		totalThis += v
	}
	totalLast := 0
	for _, v := range lastWeekOnly {
		totalLast += v
	}

	uptimeStr := w.getUptimeStr()
	return notifiers.FormatWeeklyReport(hostname, thisWeek, lastWeekOnly, totalThis, totalLast, uptimeStr)
}

func (w *TelegramBotWatcher) cmdReboot(parts []string) string {
	if len(parts) < 2 || strings.ToUpper(parts[1]) != "CONFIRM" {
		w.sendReplyWithKeyboard("⚠️ <b>Reboot requires confirmation</b>",
			[][]InlineButton{{{Text: "🔄 Reboot Now", Data: "reboot:confirm"}}})
		return ""
	}

	w.sendReply("🔄 Rebooting in 5 seconds...")

	go func() {
		time.Sleep(5 * time.Second)
		_ = exec.Command("reboot").Run()
	}()

	return ""
}

func (w *TelegramBotWatcher) cmdDoctor() string {
	results := doctor.New(w.Cfg, store.DefaultDBPath).Run()
	return doctor.RenderTelegram(results)
}

func (w *TelegramBotWatcher) cmdUpdates() string {
	out, err := exec.Command("apt-get", "list", "--upgradable").CombinedOutput()
	if err != nil {
		return "❌ Failed to check for updates"
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	// First line is usually "Listing..."
	var pkgs []string
	for _, line := range lines {
		if strings.Contains(line, "upgradable") {
			pkgs = append(pkgs, line)
		}
	}

	if len(pkgs) == 0 {
		return "✅ <b>System up to date</b>\n\nNo packages available for upgrade."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📦 <b>%d package(s) upgradable</b>\n\n", len(pkgs)))
	for _, p := range pkgs {
		// Extract just the package name (before the /)
		name := p
		if idx := strings.Index(p, "/"); idx > 0 {
			name = p[:idx]
		}
		b.WriteString(fmt.Sprintf("  • %s\n", html.EscapeString(name)))
	}
	b.WriteString("\nTo upgrade: /update CONFIRM")
	return b.String()
}

func (w *TelegramBotWatcher) cmdUpdate(parts []string) string {
	if len(parts) < 2 || strings.ToUpper(parts[1]) != "CONFIRM" {
		w.sendReplyWithKeyboard("⚠️ <b>System upgrade requires confirmation</b>\n\nThis will run <code>apt-get update && apt-get upgrade -y</code>.",
			[][]InlineButton{{{Text: "📦 Run Update", Data: "update:confirm"}}})
		return ""
	}

	w.sendReply("📦 Running system update... this may take a few minutes.")

	// apt-get update
	updateOut, err := exec.Command("apt-get", "update").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ <b>apt-get update failed</b>\n<code>%s</code>",
			truncate(html.EscapeString(strings.TrimSpace(string(updateOut))), 500))
	}

	// apt-get upgrade -y
	upgradeOut, err := exec.Command("apt-get", "upgrade", "-y").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("❌ <b>apt-get upgrade failed</b>\n<code>%s</code>",
			truncate(html.EscapeString(strings.TrimSpace(string(upgradeOut))), 500))
	}

	count := parseUpgradeCount(string(upgradeOut))
	var b strings.Builder
	if count > 0 {
		b.WriteString(fmt.Sprintf("✅ <b>%d package(s) upgraded</b>\n", count))
	} else {
		b.WriteString("✅ <b>Already up to date</b>\n")
	}

	// Check reboot-required
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		b.WriteString("\n⚠️ <b>Reboot required</b> — send /reboot CONFIRM")
	}

	return b.String()
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
