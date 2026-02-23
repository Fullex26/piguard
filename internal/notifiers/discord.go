package notifiers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/fullexpi/piguard/internal/config"
	"github.com/fullexpi/piguard/pkg/models"
)

// Discord sends notifications via Discord webhooks
type Discord struct {
	webhookURL string
	client     *http.Client
}

func NewDiscord(cfg config.DiscordConfig) *Discord {
	return &Discord{
		webhookURL: cfg.WebhookURL,
		client:     &http.Client{},
	}
}

func (d *Discord) Name() string { return "discord" }

func (d *Discord) Send(event models.Event) error {
	color := 0x3498db // blue for info
	switch event.Severity {
	case models.SeverityWarning:
		color = 0xf39c12 // orange
	case models.SeverityCritical:
		color = 0xe74c3c // red
	}

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s PiGuard — %s", event.Severity.Emoji(), event.Hostname),
		"description": event.Message,
		"color":       color,
		"fields":      []map[string]interface{}{},
	}

	fields := embed["fields"].([]map[string]interface{})
	if event.Details != "" {
		fields = append(fields, map[string]interface{}{
			"name": "Details", "value": event.Details, "inline": false,
		})
	}
	if event.Suggested != "" {
		fields = append(fields, map[string]interface{}{
			"name": "💡 Suggested", "value": event.Suggested, "inline": false,
		})
	}
	embed["fields"] = fields

	payload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}

	return d.sendJSON(payload)
}

func (d *Discord) SendRaw(message string) error {
	payload := map[string]string{"content": message}
	return d.sendJSON(payload)
}

func (d *Discord) Test() error {
	return d.SendRaw("🛡️ **PiGuard** — Test notification\n\nIf you see this, PiGuard is connected!")
}

func (d *Discord) sendJSON(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := d.client.Post(d.webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("discord send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}
	return nil
}
