package collab

import "testing"

func TestFallbackIntroContainsRoleName(t *testing.T) {
	for _, id := range MainChannelRoster {
	 intro := FallbackIntro(id)
	 if intro == "" {
	  t.Fatalf("empty fallback for %s", id)
	 }
	 name := DisplayName(id)
	 if name == "" {
	  t.Fatalf("empty display name for %s", id)
	 }
	}
}

func TestAgentRoleLabel(t *testing.T) {
	if got := AgentRoleLabel("coder-plan-demo"); got != "Senior Coder" {
		t.Fatalf("coder: got %q", got)
	}
	if got := AgentRoleLabel("tester-main"); got != "Tester" {
		t.Fatalf("tester: got %q", got)
	}
}

func TestAgentFallbackIntroNonEmpty(t *testing.T) {
	for _, id := range []string{"coder-x", "tester-y", "agent-z"} {
		if intro := AgentFallbackIntro(id); intro == "" {
			t.Fatalf("empty intro for %s", id)
		}
	}
}

func TestAssertionKeywordsNonEmpty(t *testing.T) {
	for _, id := range MainChannelRoster {
	 kw := AssertionKeywords(id)
	 if len(kw) == 0 {
	  t.Fatalf("no keywords for %s", id)
	 }
	}
}
