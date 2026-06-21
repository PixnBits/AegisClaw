package collab

import "testing"

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
	}
	for _, tc := range tests {
		if got := NormalizeMemberRole(tc.in); got != tc.want {
			t.Errorf("NormalizeMemberRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}