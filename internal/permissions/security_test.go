package permissions

import "testing"

// TestSecurity_HiddenToolsNotDiscoverableEvenWithRegistryGrant verifies anti-fingerprinting
// at the filter layer: broad discovery grant must not reveal hidden capabilities.
func TestSecurity_HiddenToolsNotDiscoverableEvenWithRegistryGrant(t *testing.T) {
	state := NewState()
	_ = GrantCapability(state, "coder*", "tool.registry.discover", "user", "test discover grant")
	SetVisibility(state, "coder*", "court.review", VisibilityHidden, "user", "hide court from coder")
	SetVisibility(state, "coder*", "secrets.push", VisibilityHidden, "user", "hide secrets from coder")

	caps := KnownCapabilities()
	f := BuildFilter(state, "coder-evil", caps)
	if !f.CanDiscover {
		t.Fatal("expected tool.registry.discover grant")
	}
	if f.VisibleTools["court.review"] {
		t.Error("court.review must stay hidden despite discover grant")
	}
	if f.VisibleTools["secrets.push"] {
		t.Error("secrets.push must stay hidden despite discover grant")
	}
}

func TestSecurity_NilStateRecordRequestRejected(t *testing.T) {
	_, err := RecordRequest(nil, "agent-1", "channel.create", "ctx")
	if err == nil {
		t.Fatal("nil state must not silently accept record request")
	}
}

// TestSecurity_SelfGrantRejected verifies microVMs cannot self-grant capabilities.
func TestSecurity_SelfGrantRejected(t *testing.T) {
	state := NewState()
	err := GrantCapability(state, "agent-rogue", "channel.create", "agent-rogue", "self")
	if err == nil {
		t.Fatal("self-grant must be rejected")
	}
	if HasGrant(state, "agent-rogue", "channel.create") {
		t.Error("self-grant must not persist")
	}
}