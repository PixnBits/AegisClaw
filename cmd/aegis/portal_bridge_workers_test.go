package main

import "testing"

func TestPortalInfraVM(t *testing.T) {
	cases := []struct {
		id, vmType string
		infra     bool
	}{
		{"store", "store", true},
		{"web-portal", "web-portal", true},
		{"memory-abc", "memory", true},
		{"project-manager-main", "project-manager", false},
		{"court-persona-ciso", "court-persona", false},
		{"agent-session1", "agent", false},
		{"coder-main", "coder", false},
	}
	for _, c := range cases {
		got := portalInfraVM(c.id, c.vmType)
		if got != c.infra {
			t.Errorf("portalInfraVM(%q, %q) = %v, want %v", c.id, c.vmType, got, c.infra)
		}
	}
}

func TestPortalVMRoleLabel(t *testing.T) {
	if got := portalVMRoleLabel("project-manager-main", "project-manager"); got != "project-manager" {
		t.Fatalf("got %q", got)
	}
	if got := portalVMRoleLabel("court-persona-ciso", "court-persona"); got != "court" {
		t.Fatalf("got %q", got)
	}
	if got := portalVMRoleLabel("agent-1", "agent"); got != "agent" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeChannelRosterIntoWorkersAddsOnDemandPM(t *testing.T) {
	workers := []interface{}{
		map[string]interface{}{"id": "court-persona-ciso", "name": "court-persona-ciso", "status": "running"},
	}
	members := []interface{}{
		map[string]interface{}{"role": "project-manager"},
		map[string]interface{}{"role": "court-persona-ciso"},
	}
	mergeChannelRosterFromMembers(&workers, "main", members)
	if len(workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(workers))
	}
	pm, ok := workers[1].(map[string]interface{})
	if !ok {
		t.Fatal("expected map worker")
	}
	if pm["name"] != "project-manager-main" {
		t.Fatalf("name=%v", pm["name"])
	}
	if pm["status"] != "standby" {
		t.Fatalf("status=%v", pm["status"])
	}
}
