package watchers

import (
	"context"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
)

// Watcher is the interface all security watchers implement
type Watcher interface {
	// Name returns the watcher identifier
	Name() string
	// Start begins watching. Blocks until context is cancelled.
	Start(ctx context.Context) error
	// Stop gracefully stops the watcher
	Stop() error
}

// Base provides common fields for all watchers
type Base struct {
	Cfg *config.Config
	Bus *eventbus.Bus
}
