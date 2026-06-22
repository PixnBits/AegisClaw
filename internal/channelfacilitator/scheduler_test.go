package channelfacilitator

import "testing"

func TestDedupeMemberRoles(t *testing.T) {
	members := []map[string]interface{}{
		{"role": "project-manager"},
		{"role": "coder"},
		{"role": "project-manager"},
		{"role": "coder"},
	}
	got := dedupeMemberRoles(members)
	if len(got) != 2 || got[0] != "project-manager" || got[1] != "coder" {
		t.Fatalf("dedupeMemberRoles() = %v, want [project-manager coder]", got)
	}
}