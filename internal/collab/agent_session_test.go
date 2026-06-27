package collab

import "testing"

func TestIsChatAgentSession(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"9e0e", true},
		{"agent-9e0e", true},
		{"court-persona-user-advocate", false},
		{"court-scribe", false},
		{"project-manager-main", false},
		{"store", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsChatAgentSession(tc.id); got != tc.want {
			t.Errorf("IsChatAgentSession(%q)=%v want %v", tc.id, got, tc.want)
		}
	}
}

func TestChatAgentSessionID(t *testing.T) {
	if got := ChatAgentSessionID("agent-abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := ChatAgentSessionID("court-persona-ciso"); got != "court-persona-ciso" {
		t.Fatalf("got %q", got)
	}
}