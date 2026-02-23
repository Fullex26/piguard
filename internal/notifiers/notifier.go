package notifiers

import "github.com/fullexpi/piguard/pkg/models"

// Notifier sends alerts to external channels
type Notifier interface {
	// Name returns the notifier identifier
	Name() string
	// Send delivers an event notification
	Send(event models.Event) error
	// SendRaw sends a pre-formatted message (for summaries)
	SendRaw(message string) error
	// Test sends a test notification to verify configuration
	Test() error
}
