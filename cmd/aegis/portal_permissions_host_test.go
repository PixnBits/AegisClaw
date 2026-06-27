package main

import "testing"

func TestParseAgentPermissionsPath(t *testing.T) {
	cases := []struct {
		path   string
		id     string
		ok     bool
	}{
		{"/api/agents/project-manager-main/permissions", "project-manager-main", true},
		{"/api/agents/court-persona-architect/permissions", "court-persona-architect", true},
		{"/api/agents/foo/trace", "", false},
		{"/api/agents/foo/permissions/extra", "", false},
	}
	for _, tc := range cases {
		id, ok := parseAgentPermissionsPath(tc.path)
		if ok != tc.ok || id != tc.id {
			t.Errorf("parseAgentPermissionsPath(%q) = %q,%v want %q,%v", tc.path, id, ok, tc.id, tc.ok)
		}
	}
}