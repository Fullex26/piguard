package eventbus

import (
	"sync"

	"github.com/Fullex26/piguard/pkg/models"
)

// Handler is a function that receives events
type Handler func(event models.Event)

// Bus is a simple in-process pub/sub event bus
type Bus struct {
	mu       sync.RWMutex
	handlers []Handler
}

// New creates a new event bus
func New() *Bus {
	return &Bus{
		handlers: make([]Handler, 0),
	}
}

// Subscribe registers a handler for all events
func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
}

// Publish sends an event to all subscribers
// Events are dispatched in goroutines to prevent blocking
func (b *Bus) Publish(event models.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, h := range b.handlers {
		go h(event)
	}
}
