package runtime

import (
	"strings"
	"testing"

	"AegisClaw/internal/channelid"
)

func TestChannelIDFitsEveryAllowlistedRole(t *testing.T) {
	roles := channelid.Roles()
	if len(roles) == 0 {
		t.Fatal("empty allowlist")
	}
	longest := ""
	for _, role := range roles {
		if len(role) > len(longest) {
			longest = role
		}
	}
	if longest != "security-architect" || len(longest) != 18 {
		t.Fatalf("longest allowlisted role = %q (%d), want security-architect (18)", longest, len(longest))
	}
	wantCap := 64 - len(longest) - 1
	if channelid.MaxChannelIDLen != wantCap {
		t.Fatalf("MaxChannelIDLen = %d, want %d (64 - %d - 1)", channelid.MaxChannelIDLen, wantCap, len(longest))
	}

	ch := strings.Repeat("a", channelid.MaxChannelIDLen)
	if err := ValidateChannelID(ch); err != nil {
		t.Fatalf("max channel: %v", err)
	}
	for _, role := range roles {
		id := role + "-" + ch
		if err := ValidateVMID(id); err != nil {
			t.Errorf("ValidateVMID(%q) = %v (len %d)", id, err, len(id))
		}
	}
	for _, prefix := range []string{"agent-", "memory-"} {
		id := prefix + ch
		if err := ValidateVMID(id); err != nil {
			t.Errorf("ValidateVMID(%q) = %v", id, err)
		}
	}

	// One byte over the cap does not fit the longest role (65 > 64).
	// A shorter role may still fit; the channel rule rejects it anyway.
	too := strings.Repeat("b", 46)
	if err := ValidateChannelID(too); err == nil {
		t.Fatal("46-char channel accepted")
	}
	over := longest + "-" + too
	if len(over) != 65 {
		t.Fatalf("longest role + 46-char channel = %d bytes, want 65", len(over))
	}
	if err := ValidateVMID(over); err == nil {
		t.Fatalf("46-char channel fits %s (%d bytes)", longest, len(over))
	}
}
