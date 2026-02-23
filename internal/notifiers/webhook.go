package notifiers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/pkg/models"
)

// Webhook sends notifications via generic HTTP webhooks
type Webhook struct {
	url    string
	method string
	client *http.Client
}

func NewWebhook(cfg config.WebhookConfig) *Webhook {
	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	return &Webhook{
		url:    cfg.URL,
		method: method,
		client: &http.Client{},
	}
}

func (w *Webhook) Name() string { return "webhook" }

func (w *Webhook) Send(event models.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return w.send(data)
}

func (w *Webhook) SendRaw(message string) error {
	payload := map[string]string{"message": message}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return w.send(data)
}

func (w *Webhook) Test() error {
	return w.SendRaw("PiGuard test notification")
}

func (w *Webhook) send(data []byte) error {
	req, err := http.NewRequest(w.method, w.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PiGuard/0.1")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
