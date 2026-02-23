package watchers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/eventbus"
	"github.com/Fullex26/piguard/pkg/models"
)

// FirewallWatcher monitors iptables chains for unexpected changes
type FirewallWatcher struct {
	Base
	baselines map[string]string // chain -> rule hash
	interval  time.Duration
}

func NewFirewallWatcher(cfg *config.Config, bus *eventbus.Bus) *FirewallWatcher {
	interval, _ := time.ParseDuration(cfg.Firewall.CheckInterval)
	if interval == 0 {
		interval = 60 * time.Second
	}

	return &FirewallWatcher{
		Base:      Base{Cfg: cfg, Bus: bus},
		baselines: make(map[string]string),
		interval:  interval,
	}
}

func (w *FirewallWatcher) Name() string { return "firewall" }

func (w *FirewallWatcher) Start(ctx context.Context) error {
	slog.Info("starting firewall watcher", "interval", w.interval)

	// Build initial baseline
	for _, chain := range w.Cfg.Firewall.Chains {
		rules, err := w.getRules(chain.Table, chain.Chain)
		if err != nil {
			slog.Warn("cannot read chain", "chain", chain.Chain, "error", err)
			continue
		}
		w.baselines[chain.Chain] = hashRules(rules)
	}

	// Check configured expectations immediately
	w.checkExpectations()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *FirewallWatcher) Stop() error { return nil }

func (w *FirewallWatcher) check() {
	w.checkExpectations()
	w.checkDrift()
}

// checkExpectations verifies expected policies and rules
func (w *FirewallWatcher) checkExpectations() {
	hostname, _ := os.Hostname()

	for _, chain := range w.Cfg.Firewall.Chains {
		rules, err := w.getRules(chain.Table, chain.Chain)
		if err != nil {
			continue
		}

		// Check expected policy
		if chain.ExpectPolicy != "" {
			policy := w.getPolicy(chain.Table, chain.Chain)
			if !strings.EqualFold(policy, chain.ExpectPolicy) {
				w.Bus.Publish(models.Event{
					ID:        fmt.Sprintf("fw-policy-%s-%d", chain.Chain, time.Now().Unix()),
					Type:      models.EventFirewallChanged,
					Severity:  models.SeverityCritical,
					Hostname:  hostname,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Firewall policy changed: %s is %s (expected %s)", chain.Chain, policy, chain.ExpectPolicy),
					Suggested: fmt.Sprintf("Run: sudo iptables -P %s %s", chain.Chain, chain.ExpectPolicy),
					Source:    "firewall",
					Firewall: &models.FirewallState{
						Chain:  chain.Chain,
						Table:  chain.Table,
						Policy: policy,
					},
				})
			}
		}

		// Check expected rule pattern
		if chain.ExpectRule != "" {
			re, err := regexp.Compile(chain.ExpectRule)
			if err != nil {
				slog.Warn("invalid expect_rule regex", "pattern", chain.ExpectRule, "error", err)
				continue
			}

			found := false
			for _, rule := range rules {
				if re.MatchString(rule) {
					found = true
					break
				}
			}

			if !found {
				w.Bus.Publish(models.Event{
					ID:        fmt.Sprintf("fw-rule-%s-%d", chain.Chain, time.Now().Unix()),
					Type:      models.EventFirewallChanged,
					Severity:  models.SeverityCritical,
					Hostname:  hostname,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Expected rule missing in %s chain (pattern: %s)", chain.Chain, chain.ExpectRule),
					Suggested: "Check your DOCKER-USER chain: sudo iptables -L DOCKER-USER -n --line-numbers",
					Source:    "firewall",
					Firewall: &models.FirewallState{
						Chain:       chain.Chain,
						Table:       chain.Table,
						HasDropRule: false,
					},
				})
			}
		}
	}
}

// checkDrift detects any change in firewall rules
func (w *FirewallWatcher) checkDrift() {
	hostname, _ := os.Hostname()

	for _, chain := range w.Cfg.Firewall.Chains {
		rules, err := w.getRules(chain.Table, chain.Chain)
		if err != nil {
			continue
		}

		currentHash := hashRules(rules)
		baselineHash, exists := w.baselines[chain.Chain]

		if exists && currentHash != baselineHash {
			w.Bus.Publish(models.Event{
				ID:        fmt.Sprintf("fw-drift-%s-%d", chain.Chain, time.Now().Unix()),
				Type:      models.EventFirewallChanged,
				Severity:  models.SeverityWarning,
				Hostname:  hostname,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("Firewall rules changed in %s chain", chain.Chain),
				Details:   "Rules differ from baseline. Run `piguard baseline reset` to accept current state.",
				Source:    "firewall",
				Firewall: &models.FirewallState{
					Chain:    chain.Chain,
					Table:    chain.Table,
					RuleHash: currentHash,
				},
			})
			// Update baseline to prevent repeated alerts
			w.baselines[chain.Chain] = currentHash
		}
	}
}

func (w *FirewallWatcher) getRules(table, chain string) ([]string, error) {
	out, err := exec.Command("iptables", "-t", table, "-L", chain, "-n").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 2 {
		return lines[2:], nil // Skip header lines
	}
	return []string{}, nil
}

func (w *FirewallWatcher) getPolicy(table, chain string) string {
	out, err := exec.Command("iptables", "-t", table, "-L", chain, "-n").Output()
	if err != nil {
		return "UNKNOWN"
	}
	firstLine := strings.Split(string(out), "\n")[0]
	// Format: Chain INPUT (policy DROP)
	if idx := strings.Index(firstLine, "policy "); idx >= 0 {
		policy := firstLine[idx+7:]
		if end := strings.Index(policy, ")"); end > 0 {
			return policy[:end]
		}
	}
	return "UNKNOWN"
}

func hashRules(rules []string) string {
	h := sha256.New()
	for _, r := range rules {
		h.Write([]byte(r))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
