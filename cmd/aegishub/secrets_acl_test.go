package main

import "testing"

// network-boundary -> store grants secrets.response exactly and no other
// secrets command; secrets.push is host-only.
func TestRepoACLBoundarySecretsNarrowed(t *testing.T) {
	loadRepoACL(t)

	if !checkACL("network-boundary", "store", "secrets.response") {
		t.Error("network-boundary -> store secrets.response = false, want allow")
	}
	for _, cmd := range []string{
		"secrets.push", "secrets.update", "secrets.get", "secrets.request",
		"secrets.status", "secrets.pushed", "secrets.x", "secrets.responsex", "secrets",
	} {
		if checkACL("network-boundary", "store", cmd) {
			t.Errorf("network-boundary -> store %s = true, want deny", cmd)
		}
	}
	// Store -> network-boundary delivery is unchanged.
	for _, cmd := range []string{"secrets.update", "secrets.push"} {
		if !checkACL("store", "network-boundary", cmd) {
			t.Errorf("store -> network-boundary %s = false, want allow", cmd)
		}
	}
}

// No store-destination rule for network-boundary or a guest source grants a
// secrets command through a wildcard, and none grants secrets.push.
func TestRepoACLNoGuestSecretsWildcardToStore(t *testing.T) {
	loadRepoACL(t)
	guests := []string{
		"network-boundary", "web-portal", "agent", "agent-1", "agent1", "coder-1", "tester-1",
		"builder", "memory", "project-manager", "project-manager-1", "court-persona-ciso",
		"court-scribe", "ciso", "ciso-1", "architect-1", "researcher-1",
	}
	for _, id := range guests {
		if checkACL(id, "store", "secrets.push") {
			t.Errorf("%s -> store secrets.push = true, want deny", id)
		}
		for _, rule := range aclRules {
			if !aclIDMatch(rule.Source, id) || !aclIDMatch(rule.Destination, "store") {
				continue
			}
			for _, c := range rule.Commands {
				if c != "secrets.response" && (aclMatch(c, "secrets.push") || aclMatch(c, "secrets.zz")) {
					t.Errorf("rule %q -> %q grants %q to %s", rule.Source, rule.Destination, c, id)
				}
				if c == "secrets.response" && id != "network-boundary" {
					t.Errorf("rule %q -> %q grants secrets.response to %s", rule.Source, rule.Destination, id)
				}
			}
		}
	}
}
