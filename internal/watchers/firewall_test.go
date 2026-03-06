package watchers

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

func TestFirewallWatcher_Name(t *testing.T) {
	w := &FirewallWatcher{}
	if got := w.Name(); got != "firewall" {
		t.Errorf("Name() = %q, want %q", got, "firewall")
	}
}

func TestHashRules(t *testing.T) {
	rules := []string{"DROP all -- 0.0.0.0/0 anywhere", "ACCEPT tcp -- 192.168.1.0/24 anywhere"}

	h1 := hashRules(rules)
	h2 := hashRules(rules)

	if h1 != h2 {
		t.Errorf("same rules produced different hashes: %q vs %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("hash length = %d, want 16", len(h1))
	}
}

func TestHashRules_DifferentInput(t *testing.T) {
	h1 := hashRules([]string{"rule A"})
	h2 := hashRules([]string{"rule B"})

	if h1 == h2 {
		t.Error("different rules should produce different hashes")
	}
}

func TestHashRules_Empty(t *testing.T) {
	h := hashRules([]string{})
	if h == "" {
		t.Error("empty rules should still produce a hash")
	}
	if len(h) != 16 {
		t.Errorf("hash length = %d, want 16", len(h))
	}
}

// ── iptables output helpers ───────────────────────────────────────────────────

// fakeIptablesOutput simulates `iptables -t <table> -L <chain> -n` output.
func fakeIptablesOutput(policy string, rules []string) []byte {
	header := fmt.Sprintf("Chain INPUT (policy %s)\ntarget     prot opt source               destination\n", policy)
	for _, r := range rules {
		header += r + "\n"
	}
	return []byte(header)
}

func newTestFirewallWatcher(cfg *config.Config, fakeExec func(table, chain string) ([]byte, error)) (*FirewallWatcher, *fwEventCapture) {
	bus := eventbus.New()
	cap := &fwEventCapture{}
	bus.Subscribe(func(e models.Event) {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		cap.events = append(cap.events, e)
	})

	w := NewFirewallWatcher(cfg, bus)
	w.execIptables = fakeExec
	return w, cap
}

type fwEventCapture struct {
	mu     sync.Mutex
	events []models.Event
}

func (c *fwEventCapture) Events() []models.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]models.Event, len(c.events))
	copy(cp, c.events)
	return cp
}

// ── Rule parsing tests ────────────────────────────────────────────────────────

func TestFirewallWatcher_GetRules(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("DROP", []string{
			"ACCEPT     all  --  0.0.0.0/0            0.0.0.0/0",
			"DROP       tcp  --  0.0.0.0/0            0.0.0.0/0",
		}), nil
	})

	rules, err := w.getRules("filter", "INPUT")
	if err != nil {
		t.Fatalf("getRules error: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
}

func TestFirewallWatcher_GetRules_EmptyChain(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("ACCEPT", nil), nil
	})

	rules, err := w.getRules("filter", "INPUT")
	if err != nil {
		t.Fatalf("getRules error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

func TestFirewallWatcher_GetRules_ExecError(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return nil, fmt.Errorf("iptables not found")
	})

	_, err := w.getRules("filter", "INPUT")
	if err == nil {
		t.Error("expected error from getRules")
	}
}

// ── Policy extraction tests ──────────────────────────────────────────────────

func TestFirewallWatcher_GetPolicy_DROP(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("DROP", nil), nil
	})

	policy := w.getPolicy("filter", "INPUT")
	if policy != "DROP" {
		t.Errorf("getPolicy = %q, want %q", policy, "DROP")
	}
}

func TestFirewallWatcher_GetPolicy_ACCEPT(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("ACCEPT", nil), nil
	})

	policy := w.getPolicy("filter", "INPUT")
	if policy != "ACCEPT" {
		t.Errorf("getPolicy = %q, want %q", policy, "ACCEPT")
	}
}

func TestFirewallWatcher_GetPolicy_Malformed(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return []byte("garbage output\n"), nil
	})

	policy := w.getPolicy("filter", "INPUT")
	if policy != "UNKNOWN" {
		t.Errorf("getPolicy = %q, want %q for malformed output", policy, "UNKNOWN")
	}
}

func TestFirewallWatcher_GetPolicy_ExecError(t *testing.T) {
	cfg := config.DefaultConfig()
	w, _ := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return nil, fmt.Errorf("error")
	})

	policy := w.getPolicy("filter", "INPUT")
	if policy != "UNKNOWN" {
		t.Errorf("getPolicy = %q, want %q on error", policy, "UNKNOWN")
	}
}

// ── Expectation checking tests ───────────────────────────────────────────────

func TestFirewallWatcher_PolicyMismatch_PublishesEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Chains = []config.ChainConfig{
		{Table: "filter", Chain: "INPUT", ExpectPolicy: "DROP"},
	}

	w, cap := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("ACCEPT", nil), nil // policy is ACCEPT, expected DROP
	})

	w.checkExpectations()
	time.Sleep(50 * time.Millisecond)

	events := cap.Events()
	found := false
	for _, e := range events {
		if e.Type == models.EventFirewallChanged && e.Severity == models.SeverityCritical {
			found = true
		}
	}
	if !found {
		t.Error("expected Critical firewall event for policy mismatch")
	}
}

func TestFirewallWatcher_PolicyMatch_NoEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Chains = []config.ChainConfig{
		{Table: "filter", Chain: "INPUT", ExpectPolicy: "DROP"},
	}

	w, cap := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("DROP", nil), nil
	})

	w.checkExpectations()
	time.Sleep(50 * time.Millisecond)

	if len(cap.Events()) > 0 {
		t.Error("expected no events when policy matches")
	}
}

func TestFirewallWatcher_MissingRule_PublishesEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Chains = []config.ChainConfig{
		{Table: "filter", Chain: "INPUT", ExpectRule: "DROP.*0.0.0.0/0"},
	}

	w, cap := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("DROP", []string{
			"ACCEPT     all  --  anywhere             anywhere",
		}), nil
	})

	w.checkExpectations()
	time.Sleep(50 * time.Millisecond)

	events := cap.Events()
	found := false
	for _, e := range events {
		if e.Type == models.EventFirewallChanged {
			found = true
		}
	}
	if !found {
		t.Error("expected firewall event for missing rule")
	}
}

func TestFirewallWatcher_RulePresent_NoEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Chains = []config.ChainConfig{
		{Table: "filter", Chain: "INPUT", ExpectRule: "DROP.*0.0.0.0/0"},
	}

	w, cap := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		return fakeIptablesOutput("DROP", []string{
			"DROP       all  --  0.0.0.0/0            0.0.0.0/0",
		}), nil
	})

	w.checkExpectations()
	time.Sleep(50 * time.Millisecond)

	if len(cap.Events()) > 0 {
		t.Error("expected no events when rule is present")
	}
}

// ── Drift detection tests ────────────────────────────────────────────────────

func TestFirewallWatcher_Drift_DetectsChange(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Chains = []config.ChainConfig{
		{Table: "filter", Chain: "INPUT"},
	}

	callCount := 0
	w, cap := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		callCount++
		if callCount <= 1 {
			return fakeIptablesOutput("DROP", []string{"rule A"}), nil
		}
		return fakeIptablesOutput("DROP", []string{"rule B"}), nil // rules changed
	})

	// Build initial baseline
	rules, _ := w.getRules("filter", "INPUT")
	w.baselines["INPUT"] = hashRules(rules)

	// Now check with changed rules
	w.checkDrift()
	time.Sleep(50 * time.Millisecond)

	events := cap.Events()
	found := false
	for _, e := range events {
		if e.Type == models.EventFirewallChanged && e.Severity == models.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected drift event when rules change")
	}
}

func TestFirewallWatcher_Drift_UpdatesBaseline(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.Chains = []config.ChainConfig{
		{Table: "filter", Chain: "INPUT"},
	}

	callCount := 0
	w, cap := newTestFirewallWatcher(cfg, func(table, chain string) ([]byte, error) {
		callCount++
		if callCount <= 1 {
			return fakeIptablesOutput("DROP", []string{"rule A"}), nil
		}
		return fakeIptablesOutput("DROP", []string{"rule B"}), nil
	})

	rules, _ := w.getRules("filter", "INPUT")
	w.baselines["INPUT"] = hashRules(rules)

	w.checkDrift()
	time.Sleep(50 * time.Millisecond)

	// After first drift, baseline should be updated, so second check shouldn't fire
	w.checkDrift()
	time.Sleep(50 * time.Millisecond)

	events := cap.Events()
	if len(events) != 1 {
		t.Errorf("expected 1 drift event (baseline updated), got %d", len(events))
	}
}
