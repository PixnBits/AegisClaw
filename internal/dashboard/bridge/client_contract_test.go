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

func TestLLMUsageBridgeActionsAreReadOnly(t *testing.T) {
	g := bridge.NewGuard()
	for _, act := range []string{"llm.usage.summary", "llm.usage.recent"} {
		if err := g.Validate(act); err != nil {
			t.Errorf("allowed action %q rejected: %v", act, err)
		}
		if !contracts.IsAllowedBridgeAction(act) {
			t.Errorf("allowlist missing %q", act)
		}
	}
	// Mutant: adding llm.usage.record (or agent settings) to the allowlist fails this test.
	for _, act := range []string{
		"llm.usage.record",
		"agent.settings.get",
		"agent.settings.set",
		"agent.soul.get",
		"agent.soul.set",
	} {
		if contracts.IsAllowedBridgeAction(act) {
			t.Errorf("allowlist must not contain %q", act)
		}
		if err := g.Validate(act); err == nil {
			t.Errorf("guard accepted %q", act)
		}
	}
}
