package notifiers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/pkg/models"
)

const telegramAPI = "https://api.telegram.org/bot%s/sendMessage"

// Telegram sends notifications via Telegram Bot API
type Telegram struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegram(cfg config.TelegramConfig) *Telegram {
	return &Telegram{
		token:  cfg.BotToken,
		chatID: cfg.ChatID,
		client: &http.Client{},
	}
}

func (t *Telegram) Name() string { return "telegram" }

func (t *Telegram) Send(event models.Event) error {
	msg := t.formatEvent(event)
	return t.send(msg)
}

func (t *Telegram) SendRaw(message string) error {
	return t.send(message)
}

func (t *Telegram) Test() error {
	return t.send("🛡️ <b>PiGuard</b> — Test notification\n\nIf you see this, PiGuard is connected!")
}

func (t *Telegram) send(text string) error {
	apiURL := fmt.Sprintf(telegramAPI, t.token)

	data := url.Values{}
	data.Set("chat_id", t.chatID)
	data.Set("parse_mode", "HTML")
	data.Set("text", text)

	resp, err := t.client.PostForm(apiURL, data)
	if err != nil {
		return fmt.Errorf("telegram send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}
	return nil
}

func (t *Telegram) formatEvent(event models.Event) string {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("%s <b>PiGuard — %s</b>\n\n", event.Severity.Emoji(), event.Hostname))

	// Main message
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", event.Message))

	// Details
	if event.Details != "" {
		b.WriteString(fmt.Sprintf("%s\n", event.Details))
	}

	// Suggested fix
	if event.Suggested != "" {
		b.WriteString(fmt.Sprintf("\n💡 <i>%s</i>", event.Suggested))
	}

	return b.String()
}

// FormatDailySummary creates a daily summary message
func FormatDailySummary(hostname string, health models.SystemHealth, lastAlert string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("✅ <b>PiGuard — %s — Daily Summary</b>\n\n", hostname))
	b.WriteString("All clear — no security events in last 24h.\n\n")
	b.WriteString("📊 <b>Status:</b>\n")
	b.WriteString(fmt.Sprintf("  Disk: %d%% | RAM: %d%%", health.DiskUsagePercent, health.MemoryUsedPercent))

	if health.CPUTempCelsius > 0 {
		b.WriteString(fmt.Sprintf(" | Temp: %.0f°C", health.CPUTempCelsius))
	}
	b.WriteString("\n")

	if health.ContainersRunning > 0 {
		b.WriteString(fmt.Sprintf("  Containers: %d running\n", health.ContainersRunning))
	}

	b.WriteString(fmt.Sprintf("  Listening ports: %d\n", health.ListeningPorts))

	if lastAlert != "" {
		b.WriteString(fmt.Sprintf("  Last alert: %s", lastAlert))
	}

	return b.String()
}

// FormatWeeklyReport creates a weekly trend report comparing this week vs last week.
func FormatWeeklyReport(hostname string, thisWeek, lastWeek map[string]int, totalThis, totalLast int, uptimeStr string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("📊 <b>PiGuard — %s — Weekly Report</b>\n\n", hostname))

	// Total trend
	trend := trendArrow(totalThis, totalLast)
	b.WriteString(fmt.Sprintf("<b>Total events:</b> %d %s (last week: %d)\n", totalThis, trend, totalLast))
	b.WriteString(fmt.Sprintf("<b>Uptime:</b> %s\n\n", uptimeStr))

	// Top event types (sorted by this week's count)
	type kv struct {
		key   string
		count int
	}
	var sorted []kv
	seen := make(map[string]bool)
	for k, v := range thisWeek {
		sorted = append(sorted, kv{k, v})
		seen[k] = true
	}
	for k, v := range lastWeek {
		if !seen[k] {
			sorted = append(sorted, kv{k, 0})
			_ = v
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	if len(sorted) > 0 {
		b.WriteString("<b>Event breakdown:</b>\n")
		limit := 10
		if len(sorted) < limit {
			limit = len(sorted)
		}
		for _, kv := range sorted[:limit] {
			prev := lastWeek[kv.key]
			trend := trendArrow(kv.count, prev)
			b.WriteString(fmt.Sprintf("  • <code>%s</code>: %d %s\n", kv.key, kv.count, trend))
		}
	} else {
		b.WriteString("✅ No events this week.\n")
	}

	return b.String()
}

func trendArrow(current, previous int) string {
	if previous == 0 && current == 0 {
		return "→"
	}
	if current > previous {
		return "↑"
	}
	if current < previous {
		return "↓"
	}
	return "→"
}
