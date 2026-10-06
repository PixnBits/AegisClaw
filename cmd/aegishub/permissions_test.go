package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestCheckHubPermission_ChannelBootstrapFallback(t *testing.T) {
	// No cached snapshot and no store — bootstrap fallback applies.
	// Denied checks spawn emitPermissionRequest; that returns immediately with no store.
	permSnapMu.Lock()
	savedSnaps := permSnapshots
	savedBootstrap := permBootstrap
	permSnapshots = map[string]permissions.Snapshot{}
	permBootstrap = permissions.DefaultBootstrap()
	permSnapMu.Unlock()

	registeredMutex.Lock()
	savedStore, hadStore := registered["store"]
	delete(registered, "store")
	registeredMutex.Unlock()

	t.Cleanup(func() {
		time.Sleep(20 * time.Millisecond)
		permSnapMu.Lock()
		permSnapshots = savedSnaps
		permBootstrap = savedBootstrap
		permSnapMu.Unlock()
		if !hadStore {
			return
		}
		registeredMutex.Lock()
		registered["store"] = savedStore
		registeredMutex.Unlock()
	})

	if allowed, reason := checkHubPermission("coder-1", "channel.add_member"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("coder-1 channel.add_member: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("tester-1", "channel.add_member"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("tester-1 channel.add_member: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("project-manager-main", "channel.add_member"); !allowed || reason != "" {
		t.Errorf("project-manager-main channel.add_member: allowed=%v reason=%q, want allowed", allowed, reason)
	}
	if allowed, reason := checkHubPermission("agent-1", "channel.turn"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("agent-1 channel.turn: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("project-manager-main", "channel.turn"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("project-manager-main channel.turn: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("agent-1", "channel.member_turn_update"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("agent-1 channel.member_turn_update: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("project-manager-main", "channel.member_turn_update"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("project-manager-main channel.member_turn_update: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("coder-1", "channel.turn_result"); !allowed || reason != "" {
		t.Errorf("coder-1 channel.turn_result: allowed=%v reason=%q, want allowed", allowed, reason)
	}
	if allowed, reason := checkHubPermission("coder-1", "channel.turn"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("coder-1 channel.turn: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("tester-1", "channel.turn"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("tester-1 channel.turn: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("coder-1", "channel.member_turn_update"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("coder-1 channel.member_turn_update: allowed=%v reason=%q, want denied", allowed, reason)
	}
	if allowed, reason := checkHubPermission("tester-1", "channel.member_turn_update"); allowed || reason != "ERR_PERMISSION_DENIED" {
		t.Errorf("tester-1 channel.member_turn_update: allowed=%v reason=%q, want denied", allowed, reason)
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
	if !shouldReceivePermissionSnapshot("project-manager") {
		t.Error("bare project-manager should receive snapshot")
	}
	if shouldReceivePermissionSnapshot("project-managerX") {
		t.Error("project-managerX must not skip into project-manager permission-subject rights")
	}
	if shouldReceivePermissionSnapshot("coderX") || shouldReceivePermissionSnapshot("testerX") {
		t.Error("undashed role lookalikes must not be permission subjects")
	}
	for _, id := range []string{"ciso", "ciso-1", "architect", "architect-x", "researcher", "researcher-y"} {
		if !shouldReceivePermissionSnapshot(id) {
			t.Errorf("%s should receive a permission snapshot", id)
		}
	}
	// Dash boundary: "cisox" is not the ciso role.
	if shouldReceivePermissionSnapshot("cisox") || shouldReceivePermissionSnapshot("architectx") || shouldReceivePermissionSnapshot("researcherx") {
		t.Error("undashed ciso/architect/researcher lookalikes must not be permission subjects")
	}
	if shouldReceivePermissionSnapshot("daemon-internal-1") {
		t.Error("daemon-internal should not receive snapshot")
	}
	for _, id := range []string{"store", "hub", "web-portal", "network-boundary", "channel-facilitator-1", "aegis-cli-internal-1", "aegis-daemon-temp-1"} {
		if shouldReceivePermissionSnapshot(id) {
			t.Errorf("host component %s must not receive a permission snapshot", id)
		}
	}
}

func TestCheckHubPermission_OnDemandRoleBootstrapFallback(t *testing.T) {
	// No cached snapshot and no store — bootstrap fallback applies.
	// Denied checks spawn emitPermissionRequest; that returns immediately with no store.
	permSnapMu.Lock()
	savedSnaps := permSnapshots
	savedBootstrap := permBootstrap
	permSnapshots = map[string]permissions.Snapshot{}
	permBootstrap = permissions.DefaultBootstrap()
	permSnapMu.Unlock()

	registeredMutex.Lock()
	savedStore, hadStore := registered["store"]
	delete(registered, "store")
	registeredMutex.Unlock()

	t.Cleanup(func() {
		time.Sleep(20 * time.Millisecond)
		permSnapMu.Lock()
		permSnapshots = savedSnaps
		permBootstrap = savedBootstrap
		permSnapMu.Unlock()
		if !hadStore {
			return
		}
		registeredMutex.Lock()
		registered["store"] = savedStore
		registeredMutex.Unlock()
	})

	// channel.add_member is a capability these roles are not granted (PM only).
	// The allowed commands are capabilities they send today that DefaultBootstrap
	// already grants to ciso*, architect*, and researcher*, same as coder/tester/agent.
	cases := []struct {
		source, command string
		wantAllowed     bool
		wantReason      string
	}{
		{"ciso-1", "channel.add_member", false, "ERR_PERMISSION_DENIED"},
		{"architect-x", "channel.add_member", false, "ERR_PERMISSION_DENIED"},
		{"researcher-y", "channel.add_member", false, "ERR_PERMISSION_DENIED"},
		{"ciso-1", "llm.call", true, ""},
		{"architect-x", "channel.get_relevant_since", true, ""},
		{"researcher-y", "channel.get_messages", true, ""},
		{"ciso-1", "channel.post", true, ""},
		{"architect-x", "channel.get", true, ""},
		{"researcher-y", "llm.call", true, ""},
	}
	for _, c := range cases {
		allowed, reason := checkHubPermission(c.source, c.command)
		if allowed != c.wantAllowed || reason != c.wantReason {
			t.Errorf("%s %s: allowed=%v reason=%q, want allowed=%v reason=%q",
				c.source, c.command, allowed, reason, c.wantAllowed, c.wantReason)
		}
	}
}

// hostACLSourcePrefixes are hub components that stay ACL-only. A source pattern
// matches when its prefix equals one of these or starts with "<prefix>-".
// aegis-daemon-temp is a host RPC client; its id does not start with "daemon".
var hostACLSourcePrefixes = []string{
	"store",
	"hub",
	"daemon",
	"channel-facilitator",
	"aegis-cli-internal",
	"network-boundary",
	"web-portal",
	"aegis-daemon-temp",
}

func TestACLVMRoleSourcesAreSnapshotSubjects(t *testing.T) {
	origRules := aclRules
	origPath := aclFilePath
	origMod := lastACLModTime
	defer func() {
		aclRules = origRules
		aclFilePath = origPath
		lastACLModTime = origMod
	}()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv("AEGIS_ACL_FILE", filepath.Join(wd, "..", "..", "config", "acls.yaml"))
	loadACL()
	if len(aclRules) == 0 {
		t.Fatal("aclRules empty after loadACL")
	}

	seen := map[string]struct{}{}
	var sources []string
	for _, rule := range aclRules {
		src := rule.Source
		if src == "*" || !strings.HasSuffix(src, "*") {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		seen[src] = struct{}{}
		sources = append(sources, src)
	}
	if len(sources) == 0 {
		t.Fatal("no starred source prefixes in config/acls.yaml")
	}

	vmCount := 0
	for _, src := range sources {
		host := isHostACLSourcePattern(src)
		if !host {
			vmCount++
		}
		t.Run(src, func(t *testing.T) {
			for _, id := range aclSourceSampleIDs(src) {
				got := shouldReceivePermissionSnapshot(id)
				if host && got {
					t.Errorf("host ACL source %q sample %q must not be a snapshot subject", src, id)
				}
				if !host && !got {
					t.Errorf("VM role ACL source %q sample %q is not a snapshot subject", src, id)
				}
			}
		})
	}
	if vmCount == 0 {
		t.Fatal("no VM role source prefixes found in config/acls.yaml")
	}
}

func isHostACLSourcePattern(pattern string) bool {
	prefix := strings.TrimSuffix(pattern, "*")
	for _, h := range hostACLSourcePrefixes {
		if prefix == h || strings.HasPrefix(prefix, h+"-") {
			return true
		}
	}
	return false
}

func aclSourceSampleIDs(pattern string) []string {
	prefix := strings.TrimSuffix(pattern, "*")
	if prefix == "" {
		return nil
	}
	if strings.HasSuffix(prefix, "-") || strings.HasSuffix(prefix, ".") {
		return []string{prefix + "1"}
	}
	return []string{prefix, prefix + "-1"}
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
