package skills

import (
	"testing"

	"AegisClaw/internal/permissions"
)

func TestPermissionFilter_GrantedVsDiscoverable(t *testing.T) {
	idx := NewAgentSkillIndex()
	idx.AddTool(Tool{Name: "channel.post", Description: "post", SkillID: "collab"})
	idx.AddTool(Tool{Name: "channel.create", Description: "create", SkillID: "collab"})
	idx.AddTool(Tool{Name: "proposal.create", Description: "proposal", SkillID: "gov"})

	idx.SetPermissionFilter(PermissionFilter{
		Enforce: true,
		AllowedTools: map[string]bool{
			"channel.post": true,
		},
		VisibleTools: map[string]bool{
			"channel.post":   true,
			"channel.create": true,
		},
		RequestableTools: map[string]bool{
			"channel.create": true,
		},
	})

	// Prompt injection: granted only
	granted := idx.ListTools()
	if len(granted) != 1 || granted[0].Name != "channel.post" {
		t.Errorf("ListTools should return granted only, got %v", granted)
	}

	// Discovery: granted + requestable
	disc := idx.ListDiscoverableTools()
	if len(disc) != 2 {
		t.Errorf("expected 2 discoverable tools, got %d", len(disc))
	}

	// Hidden tool not in search
	res := idx.SearchTools("proposal create", 5)
	for _, r := range res {
		if r.Tool.Name == "proposal.create" {
			t.Error("hidden tool must not appear in search")
		}
	}

	// Invoke check
	if err := idx.CheckToolInvoke("channel.post"); err != nil {
		t.Errorf("granted tool should pass: %v", err)
	}
	if err := idx.CheckToolInvoke("channel.create"); err == nil {
		t.Error("ungranted tool should fail invoke check")
	}
}

func TestCheckToolInvoke_RequiresVisibility(t *testing.T) {
	idx := NewAgentSkillIndex()
	idx.AddTool(Tool{Name: "proposal.create", Description: "create proposal", SkillID: "gov"})
	idx.SetPermissionFilter(PermissionFilter{
		Enforce:      true,
		AllowedTools: map[string]bool{"proposal.create": true},
		VisibleTools: map[string]bool{},
	})

	if err := idx.CheckToolInvoke("proposal.create"); err == nil {
		t.Fatal("granted-but-hidden tool must not be invokable")
	}
}

// TestSecurity_ForgedEmptySnapshot_FilterFromSnapshot ensures FilterFromSnapshot always enforces.
func TestSecurity_ForgedEmptySnapshot_FilterFromSnapshot(t *testing.T) {
	f := FilterFromSnapshot(permissions.Snapshot{})
	if !f.Enforce {
		t.Fatal("empty snapshot must enforce deny-by-default")
	}
	idx := NewAgentSkillIndex()
	idx.AddTool(Tool{Name: "proposal.create", Description: "create proposal", SkillID: "gov"})
	idx.SetPermissionFilter(f)
	if err := idx.CheckToolInvoke("proposal.create"); err == nil {
		t.Fatal("empty forged snapshot must not allow privileged invoke")
	}
}

// TestSecurity_ForgedEmptySnapshotDoesNotGrantPrivileges ensures an empty snapshot
// cannot elevate privileges when enforcement is active (deny-by-default).
func TestSecurity_ForgedEmptySnapshotDoesNotGrantPrivileges(t *testing.T) {
	idx := NewAgentSkillIndex()
	idx.AddTool(Tool{Name: "proposal.create", Description: "create proposal", SkillID: "gov"})
	idx.SetPermissionFilter(PermissionFilter{
		Enforce:      true,
		AllowedTools: map[string]bool{},
		VisibleTools: map[string]bool{},
	})

	if err := idx.CheckToolInvoke("proposal.create"); err == nil {
		t.Fatal("empty forged snapshot must not allow privileged invoke")
	}
	if len(idx.ListDiscoverableTools()) != 0 {
		t.Error("empty forged snapshot must not expose discoverable tools")
	}
}

// TestSecurity_HiddenToolsBlockedInSearch verifies index-level anti-fingerprinting.
func TestSecurity_HiddenToolsBlockedInSearch(t *testing.T) {
	state := permissions.NewState()
	_ = permissions.GrantCapability(state, "coder*", "tool.registry.discover", "user", "test")
	permissions.SetVisibility(state, "coder*", "court.review", permissions.VisibilityHidden, "user", "")

	idx := NewAgentSkillIndex()
	idx.AddTool(Tool{Name: "court.review", Description: "court review", SkillID: "gov"})
	idx.SetPermissionFilter(FilterFromPermissions(state, "coder-evil", permissions.KnownCapabilities()))

	res := idx.SearchTools("court review", 10)
	for _, r := range res {
		if r.Tool.Name == "court.review" {
			t.Error("hidden tool leaked in search")
		}
	}
}

func TestHandleToolCommand_RegistryDiscover(t *testing.T) {
	idx := NewAgentSkillIndex()
	idx.SetPermissionFilter(PermissionFilter{Enforce: true, CanDiscoverRegistry: false})
	out := HandleToolCommand("tool.registry.discover", nil, idx)
	m, ok := out.(map[string]string)
	if !ok || m["error"] != "ERR_PERMISSION_DENIED" {
		t.Errorf("expected permission denied for registry discover without grant, got %v", out)
	}

	idx.SetPermissionFilter(PermissionFilter{Enforce: true, CanDiscoverRegistry: true, VisibleTools: map[string]bool{"web_research.search": true}})
	out = HandleToolCommand("tool.registry.discover", nil, idx)
	if _, ok := out.(map[string]interface{}); !ok {
		t.Errorf("expected tools map with discover grant, got %T", out)
	}
}
