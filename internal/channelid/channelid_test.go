package channelid

import (
	"strings"
	"testing"
)

func TestValidateChannelID(t *testing.T) {
	if MaxChannelIDLen != 45 {
		t.Fatalf("MaxChannelIDLen = %d, want 45", MaxChannelIDLen)
	}
	accept := []string{"main", "plan-demo", strings.Repeat("a", MaxChannelIDLen)}
	for _, id := range accept {
		if err := ValidateChannelID(id); err != nil {
			t.Errorf("ValidateChannelID(%q) = %v", id, err)
		}
	}
	reject := []string{
		"MyProj",
		"q4_plan",
		"v1.2",
		strings.Repeat("a", MaxChannelIDLen+1),
		"",
		"coder-",
		"foo--bar",
		" coder-1",
	}
	want := "invalid channel id: must match ^[a-z][a-z0-9-]*$ and be <= 45 chars"
	for _, id := range reject {
		err := ValidateChannelID(id)
		if err == nil {
			t.Errorf("ValidateChannelID(%q) accepted", id)
			continue
		}
		if err.Error() != want {
			t.Errorf("ValidateChannelID(%q) = %q, want %q", id, err.Error(), want)
		}
	}
}

func TestMaxChannelIDLenFollowsAllowlist(t *testing.T) {
	longest := ""
	for _, role := range Roles() {
		if len(role) > len(longest) {
			longest = role
		}
	}
	if longest != "security-architect" {
		t.Fatalf("longest role = %q", longest)
	}
	if got := 64 - len(longest) - 1; got != MaxChannelIDLen {
		t.Fatalf("cap = %d, computed %d", MaxChannelIDLen, got)
	}
	if !RoleAllowed(longest) || RoleAllowed("Coder") || RoleAllowed("court-persona-ciso") {
		t.Fatalf("allowlist mismatch")
	}
}
