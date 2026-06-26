package collab

import "testing"

func TestChannelMemberAgentID(t *testing.T) {
	tests := []struct {
		role, channel, want string
	}{
		{"project-manager", "main", "project-manager-main"},
		{"court-persona-ciso", "main", "court-persona-ciso"},
		{"coder", "main", "coder-main"},
	}
	for _, tc := range tests {
		if got := ChannelMemberAgentID(tc.role, tc.channel); got != tc.want {
			t.Errorf("ChannelMemberAgentID(%q, %q) = %q, want %q", tc.role, tc.channel, got, tc.want)
		}
	}
}

func TestNormalizeMemberRole(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Senior Coder", "coder"},
		{"coder", "coder"},
		{"court-persona-senior-coder", "court-persona-senior-coder"},
		{"Project Manager", "project-manager"},
		{"CISO", "ciso"},
		{"user:alice", "user:alice"},
		{"Someone", "user:someone"},
	}
	for _, tc := range tests {
		if got := NormalizeMemberRole(tc.in); got != tc.want {
			t.Errorf("NormalizeMemberRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}