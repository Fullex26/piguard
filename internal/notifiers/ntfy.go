package notifiers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/pkg/models"
)

// Ntfy sends notifications via ntfy.sh
type Ntfy struct {
	server string
	topic  string
	token  string
	client *http.Client
}

func NewNtfy(cfg config.NtfyConfig) *Ntfy {
	server := cfg.Server
	if server == "" {
		server = "https://ntfy.sh"
	}
	return &Ntfy{
		server: server,
		topic:  cfg.Topic,
		token:  cfg.Token,
		client: &http.Client{},
	}
}

func (n *Ntfy) Name() string { return "ntfy" }

func (n *Ntfy) Send(event models.Event) error {
	title := fmt.Sprintf("PiGuard — %s", event.Hostname)
	body := event.Message
	if event.Details != "" {
		body += "\n" + event.Details
	}
	if event.Suggested != "" {
		body += "\n\n💡 " + event.Suggested
	}

	priority := "default"
	tags := "shield"
	switch event.Severity {
	case models.SeverityCritical:
		priority = "urgent"
		tags = "rotating_light"
	case models.SeverityWarning:
		priority = "high"
		tags = "warning"
	}

	return n.send(title, body, priority, tags)
}

func (n *Ntfy) SendRaw(message string) error {
	return n.send("PiGuard", message, "default", "shield")
}

func (n *Ntfy) Test() error {
	return n.send("PiGuard", "Test notification — PiGuard is connected!", "default", "white_check_mark")
}

func (n *Ntfy) send(title, body, priority, tags string) error {
	url := fmt.Sprintf("%s/%s", n.server, n.topic)
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)

	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}
	return nil
}
