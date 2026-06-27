package main

import (
	"testing"

	"AegisClaw/internal/permissions"
)

func TestCheckHubPermission_SkipsHostComponents(t *testing.T) {
	allowed, reason := checkHubPermission("daemon-internal-1", "channel.create")
	if !allowed || reason != "" {
		t.Fatalf("daemon-internal should bypass capability checks, got allowed=%v reason=%q", allowed, reason)
	}
	allowed, reason = checkHubPermission("web-portal", "proposal.create")
	if !allowed {
		t.Fatal("web-portal should bypass capability checks")
	}
}

func TestHubPermissionAllowed_BootstrapFallback(t *testing.T) {
	// No cached snapshot and no store — bootstrap fallback applies.
	permSnapshots = map[string]permissions.Snapshot{}
	if !hubPermissionAllowed("project-manager-main", "channel.post") {
		t.Error("bootstrap fallback should allow project-manager channel.post")
	}
	if !hubPermissionAllowed("project-manager-main", "llm.call") {
		t.Error("bootstrap fallback should allow project-manager llm.call")
	}
	if !hubPermissionAllowed("court-persona-senior-coder", "channel.get_relevant_since") {
		t.Error("bootstrap fallback should allow court persona channel.get_relevant_since")
	}
}

func TestHubPermissionAllowed_EmptyCachedSnapshotDenies(t *testing.T) {
	// Cached deny-all snapshot from Store must not fall back to bootstrap.
	permSnapshots = map[string]permissions.Snapshot{
		"project-manager-main": {
			Subject:      "project-manager-main",
			AllowedTools: map[string]bool{},
			VisibleTools: map[string]bool{},
		},
	}
	if hubPermissionAllowed("project-manager-main", "channel.post") {
		t.Error("empty cached snapshot must deny even when bootstrap would allow")
	}
}

func TestShouldReceivePermissionSnapshot(t *testing.T) {
	if !shouldReceivePermissionSnapshot("agent-abc") {
		t.Error("agent should receive snapshot")
	}
	if !shouldReceivePermissionSnapshot("project-manager-1") {
		t.Error("PM should receive snapshot")
	}
	if shouldReceivePermissionSnapshot("daemon-internal-1") {
		t.Error("daemon-internal should not receive snapshot")
	}
}

func TestMaybeInvalidatePermissionsFromReply(t *testing.T) {
	// RPC reply path must trigger invalidation (Portal grant flow).
	reply := Message{
		Command: "permission.granted",
		Payload: map[string]interface{}{"subject": "coder-test", "capability": "channel.create"},
	}
	// Should not panic; full push requires live store connection.
	maybeInvalidatePermissionsFromReply(reply)
}

func TestInvalidateMatchingPermissionSnapshots_Wildcard(t *testing.T) {
	permSnapshots = map[string]permissions.Snapshot{
		"coder-a": {Subject: "coder-a"},
		"coder-b": {Subject: "coder-b"},
		"agent-x": {Subject: "agent-x"},
	}
	registeredMutex.Lock()
	registered["coder-a"] = &RegisteredComponent{ID: "coder-a"}
	registered["coder-b"] = &RegisteredComponent{ID: "coder-b"}
	registered["agent-x"] = &RegisteredComponent{ID: "agent-x"}
	registeredMutex.Unlock()

	invalidateMatchingPermissionSnapshots("coder*")
	// Snapshots are refetched async; function should at least run without error.
}
