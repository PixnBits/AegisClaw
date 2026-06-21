package permissions

import "testing"

func TestSubjectMatches(t *testing.T) {
	cases := []struct{ subject, pattern string; want bool }{
		{"project-manager-abc", "project-manager*", true},
		{"coder-xyz", "project-manager*", false},
		{"agent-1", "agent*", true},
		{"agent-1", "agent-1", true},
		{"agent-2", "agent-1", false},
		{"anything", "*", true},
	}
	for _, c := range cases {
		if got := SubjectMatches(c.subject, c.pattern); got != c.want {
			t.Errorf("SubjectMatches(%q,%q)=%v want %v", c.subject, c.pattern, got, c.want)
		}
	}
}

func TestBuildFilter_DualFiltering(t *testing.T) {
	state := NewState()
	_ = GrantCapability(state, "coder*", "channel.post", "user", "test grant")
	SetVisibility(state, "coder*", "channel.post", VisibilityPublic, "user", "")
	SetVisibility(state, "coder*", "proposal.create", VisibilityHidden, "user", "hide from coder")
	SetVisibility(state, "coder*", "channel.create", VisibilityRequestable, "user", "requestable")

	caps := KnownCapabilities()
	f := BuildFilter(state, "coder-abc123", caps)

	if !f.AllowedTools["channel.post"] {
		t.Error("expected channel.post granted")
	}
	if f.AllowedTools["channel.create"] {
		t.Error("channel.create should not be granted")
	}
	if !f.VisibleTools["channel.create"] {
		t.Error("channel.create should be visible (requestable)")
	}
	if f.RequestableTools["channel.create"] != true {
		t.Error("channel.create should be requestable")
	}
	if f.VisibleTools["proposal.create"] {
		t.Error("proposal.create must be hidden from coder (anti-fingerprinting)")
	}
}

func TestGrantCapability_RejectsSelfGrant(t *testing.T) {
	state := NewState()
	err := GrantCapability(state, "coder-1", "channel.create", "coder-1", "self")
	if err == nil {
		t.Fatal("expected self-grant rejection")
	}
}

func TestDefaultBootstrap_HidesHighPrivilege(t *testing.T) {
	state := DefaultBootstrap()
	f := BuildFilter(state, "coder-test", KnownCapabilities())
	if f.VisibleTools["permission.grant"] {
		t.Error("permission.grant must be hidden from coder")
	}
	if f.VisibleTools["court.review"] {
		t.Error("court.review must be hidden from coder")
	}
}

func TestRecordRequest(t *testing.T) {
	state := NewState()
	req := RecordRequest(state, "agent-1", "channel.create", "need to create channel for task")
	if req.Status != "pending" {
		t.Errorf("expected pending, got %s", req.Status)
	}
	if len(state.Requests) != 1 {
		t.Error("request not stored")
	}
}

func TestIsCapabilityCommand(t *testing.T) {
	if !IsCapabilityCommand("channel.create") {
		t.Error("channel.create is a capability")
	}
	if IsCapabilityCommand("tool.list") {
		t.Error("tool.list is safe discovery, not capability-gated")
	}
	if IsCapabilityCommand("channel.activity") {
		t.Error("channel.activity is collaboration delivery, not capability-gated")
	}
	if !IsCapabilityCommand("llm.call") {
		t.Error("llm.call is a capability")
	}
	if IsCapabilityCommand("register") {
		t.Error("register is not capability-gated")
	}
}
