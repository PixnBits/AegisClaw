//go:build integration

package permissions_test

import (
	"testing"

	"AegisClaw/internal/agent/skills"
	"AegisClaw/internal/permissions"
)

// TestIntegration_PMDvsCoderVisibility verifies differentiated persona filtering:
// PM sees channel.create (bootstrap grant); Coder does not see hidden court tools.
func TestIntegration_PMDvsCoderVisibility(t *testing.T) {
	state := permissions.DefaultBootstrap()
	caps := permissions.KnownCapabilities()

	pmFilter := skills.FilterFromPermissions(state, "project-manager-test", caps)
	coderFilter := skills.FilterFromPermissions(state, "coder-test", caps)

	if !pmFilter.AllowedTools["channel.create"] {
		t.Error("PM should have channel.create bootstrap grant")
	}
	if coderFilter.AllowedTools["channel.create"] {
		t.Error("Coder should not have channel.create grant by default")
	}
	if coderFilter.VisibleTools["court.review"] {
		t.Error("Coder must not discover hidden court.review (anti-fingerprinting)")
	}
	if coderFilter.VisibleTools["permission.grant"] {
		t.Error("Coder must not discover permission.grant")
	}
}

// TestIntegration_PermissionRequestOnDenied simulates denied tool attempt recording.
func TestIntegration_PermissionRequestOnDenied(t *testing.T) {
	state := permissions.NewState()
	_ = permissions.GrantCapability(state, "coder*", "channel.post", "user", "")

	idx := skills.NewAgentSkillIndex()
	idx.SetPermissionFilter(skills.FilterFromPermissions(state, "coder-1", permissions.KnownCapabilities()))

	if err := idx.CheckToolInvoke("channel.create"); err == nil {
		t.Fatal("ungranted channel.create must be denied")
	}
	req, err := permissions.RecordRequest(state, "coder-1", "channel.create", "need to create task channel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Status != "pending" {
		t.Errorf("expected pending request, got %s", req.Status)
	}
}

// TestIntegration_RegistryDiscoverRequiresGrant verifies broad discovery is grant-gated.
func TestIntegration_RegistryDiscoverRequiresGrant(t *testing.T) {
	state := permissions.DefaultBootstrap()
	idx := skills.NewAgentSkillIndex()
	idx.SetPermissionFilter(skills.FilterFromPermissions(state, "coder-1", permissions.KnownCapabilities()))

	out := skills.HandleToolCommand("tool.registry.discover", nil, idx)
	if m, ok := out.(map[string]string); !ok || m["error"] != "ERR_PERMISSION_DENIED" {
		t.Fatalf("expected ERR_PERMISSION_DENIED without discover grant, got %v", out)
	}

	_ = permissions.GrantCapability(state, "coder*", "tool.registry.discover", "user", "test")
	idx.SetPermissionFilter(skills.FilterFromPermissions(state, "coder-1", permissions.KnownCapabilities()))
	out = skills.HandleToolCommand("tool.registry.discover", nil, idx)
	if _, ok := out.(map[string]interface{}); !ok {
		t.Fatalf("expected tools map with discover grant, got %T", out)
	}
}
