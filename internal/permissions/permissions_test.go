package permissions

import (
	"encoding/json"
	"testing"
)

func TestSubjectMatches(t *testing.T) {
	cases := []struct {
		subject, pattern string
		want             bool
	}{
		{"project-manager-abc", "project-manager*", true},
		{"project-manager", "project-manager*", true},
		{"project-manager-1", "project-manager*", true},
		{"project-managerX", "project-manager*", false},
		{"coder-xyz", "project-manager*", false},
		{"agent-1", "agent*", true},
		{"agent", "agent*", true},
		{"agentX", "agent*", false},
		{"agent-1", "agent-1", true},
		{"agent-2", "agent-1", false},
		{"anything", "*", true},
		{"court-persona-x", "court-persona-*", true},
		{"court-persona-ciso", "court-persona-*", true},
		{"court-persona", "court-persona-*", false},
		{"memory.get_context", "memory.*", true},
		{"memoryfoo", "memory.*", false},
	}
	for _, c := range cases {
		if got := SubjectMatches(c.subject, c.pattern); got != c.want {
			t.Errorf("SubjectMatches(%q,%q)=%v want %v", c.subject, c.pattern, got, c.want)
		}
	}
}

func TestPersonaPattern_DoesNotWidenUndashedIDs(t *testing.T) {
	if got := PersonaPattern("project-manager-abc"); got != "project-manager-*" {
		t.Fatalf("PersonaPattern(project-manager-abc)=%q", got)
	}
	// Last-dash split of "project-managerX" is "project-*", not the PM wildcard.
	// That derived id must not inherit project-manager* grants.
	got := PersonaPattern("project-managerX")
	if got == "project-manager*" || SubjectMatches("project-managerX", "project-manager*") {
		t.Fatalf("lookalike mapped onto project-manager*: pattern %q", got)
	}
	state := DefaultBootstrap()
	if HasGrant(state, "project-managerX", "channel.post") || HasGrant(state, got, "channel.post") {
		t.Fatalf("project-managerX (persona %q) inherited channel.post", got)
	}
	if !HasGrant(state, "project-manager", "channel.post") || !HasGrant(state, "project-manager-1", "channel.post") {
		t.Fatal("real project-manager ids should keep channel.post")
	}
}

func TestIsMicroVMSource_DashBoundary(t *testing.T) {
	if !IsMicroVMSourcePublic("project-manager") || !IsMicroVMSourcePublic("project-manager-1") {
		t.Fatal("project-manager and project-manager-1 are microVM sources")
	}
	if IsMicroVMSourcePublic("project-managerX") {
		t.Fatal("project-managerX must not be treated as project-manager")
	}
	if !IsMicroVMSourcePublic("court-persona-ciso") {
		t.Fatal("court-persona-ciso is a microVM source")
	}
	for _, id := range []string{"ciso", "ciso-1", "architect", "architect-x", "researcher", "researcher-y"} {
		if !IsMicroVMSourcePublic(id) {
			t.Errorf("%s should be a microVM source", id)
		}
	}
	for _, id := range []string{"cisox", "architectx", "researcherx", "store", "hub", "web-portal", "network-boundary", "daemon-internal-1", "channel-facilitator-1", "aegis-cli-internal-1"} {
		if IsMicroVMSourcePublic(id) {
			t.Errorf("%s must not be treated as a microVM source", id)
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

func TestBuildFilter_GrantedButHiddenNotInvokable(t *testing.T) {
	state := NewState()
	_ = GrantCapability(state, "coder*", "proposal.create", "user", "granted but hidden")
	SetVisibility(state, "coder*", "proposal.create", VisibilityHidden, "user", "anti-fingerprinting")

	f := BuildFilter(state, "coder-abc", KnownCapabilities())
	if !f.AllowedTools["proposal.create"] {
		t.Error("proposal.create should remain granted")
	}
	if f.VisibleTools["proposal.create"] {
		t.Error("hidden granted capability must not be visible")
	}
}

func TestGrantCapability_NilState(t *testing.T) {
	err := GrantCapability(nil, "coder*", "channel.post", "user", "")
	if err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestRecordRequest(t *testing.T) {
	state := NewState()
	req, err := RecordRequest(state, "agent-1", "channel.create", "need to create channel for task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	if !IsCapabilityCommand("channel.turn") {
		t.Error("channel.turn is a capability")
	}
	if IsCapabilityCommand("channel.turn_result") {
		t.Error("channel.turn_result is collaboration delivery, not capability-gated")
	}
	if !IsCapabilityCommand("channel.member_turn_update") {
		t.Error("channel.member_turn_update is a capability")
	}
	if !IsCapabilityCommand("channel.add_member") {
		t.Error("channel.add_member is a capability")
	}
	// channel.post is a capability. It is in KnownCapabilities, and
	// DefaultBootstrap grants it to the roles that post (project-manager*,
	// agent*, coder*, tester*, researcher*, architect*, ciso*,
	// court-persona*). The ACL-only exclusion list is delivery and events
	// (channel.posted, channel.activity, channel.turn_result), not the post
	// action. PR #107 removed only channel.turn_result from this set.
	if !IsCapabilityCommand("channel.post") {
		t.Error("channel.post is a capability")
	}
	// channel.remove_member is a capability for the same reason as
	// channel.add_member: membership mutation, not delivery plumbing.
	// PR #107 kept membership changes capability-gated. remove_member is
	// deliberately absent from KnownCapabilities and DefaultBootstrap, so
	// no role VM holds a grant. The sender is web-portal, which is not a
	// snapshot subject. ACL-only would let agent* and project-manager*
	// through the broad channel.* store rules.
	if !IsCapabilityCommand("channel.remove_member") {
		t.Error("channel.remove_member is a capability")
	}
	if !IsCapabilityCommand("llm.call") {
		t.Error("llm.call is a capability")
	}
	if IsCapabilityCommand("register") {
		t.Error("register is not capability-gated")
	}
}

func TestDefaultBootstrap_ChannelAddMemberPMOnly(t *testing.T) {
	state := DefaultBootstrap()
	caps := KnownCapabilities()

	pm := BuildSnapshot(state, "project-manager-main", caps)
	if !pm.AllowedTools["channel.add_member"] {
		t.Error("project-manager-main should be allowed channel.add_member")
	}
	coder := BuildSnapshot(state, "coder-1", caps)
	if coder.AllowedTools["channel.add_member"] {
		t.Error("coder-1 should not be allowed channel.add_member")
	}
	tester := BuildSnapshot(state, "tester-1", caps)
	if tester.AllowedTools["channel.add_member"] {
		t.Error("tester-1 should not be allowed channel.add_member")
	}
	agent := BuildSnapshot(state, "agent-1", caps)
	if agent.AllowedTools["channel.turn"] || agent.AllowedTools["channel.member_turn_update"] {
		t.Error("agent-1 should not be allowed channel.turn or channel.member_turn_update")
	}
	if pm.AllowedTools["channel.turn"] || pm.AllowedTools["channel.member_turn_update"] {
		t.Error("project-manager-main should not be allowed channel.turn or channel.member_turn_update")
	}
}

func TestCisoDelegationOptInAndGrantFlow(t *testing.T) {
	state := DefaultBootstrap()
	if state.CisoDelegationEnabled {
		t.Error("default must be disabled")
	}
	// CISO cannot grant when disabled
	if AllowsCisoDelegation("court-persona-ciso-1", state.CisoDelegationEnabled) {
		t.Error("should not allow when disabled")
	}
	state.CisoDelegationEnabled = true
	if !AllowsCisoDelegation("court-persona-ciso-1", state.CisoDelegationEnabled) {
		t.Error("should allow CISO when enabled")
	}
	// Simulate grant by CISO source (the store guard uses this)
	err := GrantCapability(state, "coder-test", "channel.create", "court-persona-ciso-foo", "via delegation")
	if err != nil {
		t.Fatalf("ciso grant should succeed when enabled: %v", err)
	}
	grants := ListGrantsForSubject(state, "coder-test")
	found := false
	for _, g := range grants {
		if g.Capability == "channel.create" {
			found = true
		}
	}
	if !found {
		t.Error("expected grant after ciso delegation action")
	}
	// snapshot reflects
	snap := BuildSnapshot(state, "coder-test", KnownCapabilities())
	if !snap.AllowedTools["channel.create"] {
		t.Error("snapshot should show allowed after grant")
	}
	b, _ := json.MarshalIndent(state, "", "  ")
	t.Log("delegation flow state sample:", string(b)[:200])
}
