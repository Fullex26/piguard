package analysers

import (
	"strings"
	"sync"
	"time"

	"github.com/Fullex26/piguard/pkg/models"
)

// Deduplicator prevents alert spam by tracking recent alerts
type Deduplicator struct {
	mu       sync.Mutex
	seen     map[string]time.Time // dedup key -> last sent time
	cooldown time.Duration
}

func NewDeduplicator(cooldown time.Duration) *Deduplicator {
	return &Deduplicator{
		seen:     make(map[string]time.Time),
		cooldown: cooldown,
	}
}

// ShouldAlert returns true if this event hasn't been sent recently.
// All severities — including Critical — respect the cooldown so that a
// persistent condition (e.g. a missing firewall rule) does not flood alerts.
// The first occurrence of any event always gets through (key not yet seen).
func (d *Deduplicator) ShouldAlert(event models.Event) bool {
	key := d.dedupKey(event)

	d.mu.Lock()
	defer d.mu.Unlock()

	lastSent, exists := d.seen[key]
	if !exists || time.Since(lastSent) > d.cooldown {
		d.seen[key] = time.Now()
		return true
	}

	return false
}

// Cleanup removes expired entries to prevent memory leak
func (d *Deduplicator) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for key, lastSent := range d.seen {
		if time.Since(lastSent) > d.cooldown*2 {
			delete(d.seen, key)
		}
	}
}

// dedupKey generates a stable key for deduplication
func (d *Deduplicator) dedupKey(event models.Event) string {
	switch event.Type {
	case models.EventPortOpened, models.EventPortClosed:
		if event.Port != nil {
			// Wildcard-exposed ports (0.0.0.0 and ::) are often bound on both
			// IPv4 and IPv6 by the same process simultaneously. Normalize to
			// portNum+process so dual-stack binds produce a single alert.
			if event.Port.IsExposed {
				return string(event.Type) + ":exposed:" + portFromAddr(event.Port.Address) + ":" + event.Port.ProcessName
			}
			return string(event.Type) + ":" + event.Port.Address
		}
	case models.EventFirewallChanged:
		if event.Firewall != nil {
			return string(event.Type) + ":" + event.Firewall.Chain
		}
	}
	return string(event.Type) + ":" + event.Message
}

// portFromAddr extracts the port number from an address string.
// Works for both "0.0.0.0:3001" and ":::3001" formats.
func portFromAddr(addr string) string {
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		return addr[idx+1:]
	}
	return addr
}
