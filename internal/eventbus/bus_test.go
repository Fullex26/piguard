package eventbus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Fullex26/piguard/pkg/models"
)

func TestNew(t *testing.T) {
	bus := New()
	if bus == nil {
		t.Fatal("New() returned nil")
	}
}

func TestSubscribe_And_Publish(t *testing.T) {
	bus := New()
	received := make(chan models.Event, 1)

	bus.Subscribe(func(e models.Event) {
		received <- e
	})

	want := models.Event{ID: "test-1", Message: "hello"}
	bus.Publish(want)

	select {
	case got := <-received:
		if got.ID != want.ID || got.Message != want.Message {
			t.Errorf("received event = %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublish_MultipleSubscribers(t *testing.T) {
	bus := New()
	var count atomic.Int32

	for range 3 {
		bus.Subscribe(func(e models.Event) {
			count.Add(1)
		})
	}

	bus.Publish(models.Event{ID: "multi"})

	// Wait for all handlers
	deadline := time.After(time.Second)
	for count.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("only %d/3 subscribers received the event", count.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestPublish_NoSubscribers(t *testing.T) {
	bus := New()
	// Should not panic
	bus.Publish(models.Event{ID: "no-subs"})
}

func TestPublish_EventDataIntegrity(t *testing.T) {
	bus := New()
	received := make(chan models.Event, 1)

	bus.Subscribe(func(e models.Event) {
		received <- e
	})

	want := models.Event{
		ID:       "integrity-1",
		Type:     models.EventPortOpened,
		Severity: models.SeverityWarning,
		Hostname: "pi",
		Message:  "port opened",
		Details:  "0.0.0.0:8080",
	}
	bus.Publish(want)

	select {
	case got := <-received:
		if got.ID != want.ID || got.Type != want.Type || got.Severity != want.Severity ||
			got.Hostname != want.Hostname || got.Message != want.Message || got.Details != want.Details {
			t.Errorf("event data mismatch:\ngot  %+v\nwant %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestSubscribe_ConcurrentSafety(t *testing.T) {
	bus := New()
	var wg sync.WaitGroup

	// Concurrent subscribes and publishes
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			bus.Subscribe(func(e models.Event) {})
			bus.Publish(models.Event{ID: "concurrent"})
		}(i)
	}

	wg.Wait()
}
