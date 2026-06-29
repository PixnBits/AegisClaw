// Contract tests: portal bridge action allow-list per docs/specs/web-portal/test-contracts.md
package bridge_test

import (
	"testing"

	"AegisClaw/internal/dashboard/bridge"
	"AegisClaw/internal/dashboard/contracts"
)

func TestOnlyAllowedBridgeActionsPassGuard(t *testing.T) {
	g := bridge.NewGuard()
	for _, action := range contracts.AllowedBridgeActions() {
		if err := g.Validate(action); err != nil {
			t.Errorf("allowed action rejected: %s: %v", action, err)
		}
	}
}

func TestDisallowedBridgeActionsRejected(t *testing.T) {
	g := bridge.NewGuard()
	bad := []string{"", "daemon.shutdown", "store.wipe", "agent.exec"}
	for _, action := range bad {
		if err := g.Validate(action); err == nil {
			t.Errorf("expected rejection for %q", action)
		}
	}
}

func TestHighImpactActionsRequireConfirmation(t *testing.T) {
	g := bridge.NewGuard()
	for action := range contracts.HighImpactActions {
		if !g.NeedsConfirmation(action) {
			t.Errorf("expected confirmation for %s", action)
		}
	}
	if g.NeedsConfirmation("channel.list") {
		t.Error("channel.list should not require confirmation")
	}
}

func TestAgentSettingsAndLLMUsageBridgeActionsAllowed(t *testing.T) {
	// Phase 2/1 coverage for new per-agent settings + metrics bridge actions.
	g := bridge.NewGuard()
	for _, act := range []string{
		"agent.settings.get", "agent.soul.get",
		"llm.usage.summary", "llm.usage.recent", "llm.usage.record",
	} {
		if err := g.Validate(act); err != nil {
			t.Errorf("new allowed action %q rejected: %v", act, err)
		}
	}
	for _, act := range []string{"agent.settings.set", "agent.soul.set"} {
		if err := g.Validate(act); err != nil {
			t.Errorf("write action %q should be allowed: %v", act, err)
		}
		if !g.NeedsConfirmation(act) {
			t.Errorf("write action %q should require confirmation", act)
		}
	}
}