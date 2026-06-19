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
