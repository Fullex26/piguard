package eventbus

import (
	"sync"

	"github.com/Fullex26/piguard/pkg/models"
)

// Handler is a function that receives events
type Handler func(event models.Event)

const subscriberQueueSize = 256

type subscription struct {
	handler Handler
	events  chan models.Event
}

// Bus is a simple in-process pub/sub event bus
type Bus struct {
	mu            sync.RWMutex
	subscriptions []*subscription
}

// New creates a new event bus
func New() *Bus {
	return &Bus{
		subscriptions: make([]*subscription, 0),
	}
}

// Subscribe registers a handler for all events
func (b *Bus) Subscribe(h Handler) {
	sub := &subscription{
		handler: h,
		events:  make(chan models.Event, subscriberQueueSize),
	}
	go func() {
		for event := range sub.events {
			sub.handler(event)
		}
	}()

	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscriptions = append(b.subscriptions, sub)
}

// Publish sends an event to all subscribers
// Each subscriber processes a private queue, preserving publication order while
// preventing normal handler latency from blocking other subscribers.
func (b *Bus) Publish(event models.Event) {
	b.mu.RLock()
	subscriptions := append([]*subscription(nil), b.subscriptions...)
	b.mu.RUnlock()

	for _, sub := range subscriptions {
		sub.events <- event
	}
}
