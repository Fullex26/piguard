package watchers

import "testing"

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
