package runtime

import (
	"errors"
	"strings"
	"testing"

	"AegisClaw/internal/hubids"
)

func TestReservedVMIDReason(t *testing.T) {
	cases := []struct {
		id     string
		want   bool
		reason string
	}{
		{id: "store", want: true, reason: "reserved prefix store"},
		{id: "STORE", want: true, reason: "reserved prefix store"},
		{id: "store-x", want: true, reason: "reserved prefix store"},
		{id: "storefront", want: false},
		{id: "court", want: true, reason: "reserved prefix court"},
		{id: "court-persona-ciso", want: true, reason: "reserved prefix court"},
		{id: "Court-Scribe", want: true, reason: "reserved prefix court"},
		{id: "court-scribe", want: true, reason: "reserved prefix court"},
		{id: "courtyard", want: false},
		{id: "network-boundary", want: true, reason: "reserved prefix network-boundary"},
		{id: "network-boundary-edge", want: true, reason: "reserved prefix network-boundary"},
		{id: "web-portal", want: true, reason: "reserved prefix web-portal"},
		{id: "Web-Portal", want: true, reason: "reserved prefix web-portal"},
		{id: "webportal", want: false},
		{id: "builder", want: true, reason: "reserved prefix builder"},
		{id: "builder-job", want: true, reason: "reserved prefix builder"},
		{id: "builders", want: false},
		{id: "memory", want: true, reason: "reserved prefix memory"},
		{id: "memory-abc", want: true, reason: "reserved prefix memory"},
		{id: "memoryless", want: false},
		{id: "agent", want: true, reason: "reserved prefix agent"},
		{id: "agent-sess", want: true, reason: "reserved prefix agent"},
		{id: "agents", want: false},
		{id: "hub", want: true, reason: "reserved prefix hub"},
		{id: "hub-perm-fetch", want: true, reason: "reserved prefix hub-perm-fetch"},
		{id: "hub-perm-fetch-99", want: true, reason: "reserved prefix hub-perm-fetch"},
		{id: "aegishub", want: true, reason: "reserved prefix aegishub"},
		{id: "aegishub-extra", want: true, reason: "reserved prefix aegishub"},
		{id: "daemon", want: true, reason: "reserved prefix daemon"},
		{id: "daemon-orchestrator", want: true, reason: "reserved prefix daemon"},
		{id: "daemon-internal-1", want: true, reason: "reserved prefix daemon-internal"},
		{id: "aegis-cli-internal", want: true, reason: "reserved prefix aegis-cli-internal"},
		{id: "aegis-cli-internal-5", want: true, reason: "reserved prefix aegis-cli-internal"},
		{id: "channel-facilitator", want: true, reason: "reserved prefix channel-facilitator"},
		{id: "channel-facilitator-out-1", want: true, reason: "reserved prefix channel-facilitator"},
		{id: "", want: true, reason: "empty id"},
		{id: "-persona-ciso", want: true, reason: "empty id segment"},
		{id: "coder-", want: true, reason: "empty id segment"},
		{id: "foo--bar", want: true, reason: "empty id segment"},
		{id: "has space", want: true, reason: "invalid characters"},
		{id: "sl/ash", want: true, reason: "invalid characters"},
		{id: strings.Repeat("a", 128), want: false},
		{id: strings.Repeat("a", 129), want: true, reason: "id longer than 128 bytes"},
		{id: "coder", want: false},
		{id: "coder-plan-1", want: false},
		{id: "tester", want: false},
		{id: "ciso", want: false},
		{id: "security-architect", want: false},
		{id: "architect", want: false},
		{id: "efficiency", want: false},
		{id: "user-advocate", want: false},
		{id: "researcher", want: false},
		{id: "project-manager", want: false},
		{id: "project-manager-main", want: false},
		{id: "ok_Name.1", want: false},
		{id: "senior-coder", want: false},
	}
	for _, tc := range cases {
		got, reason := ReservedVMIDReason(tc.id)
		if got != tc.want || reason != tc.reason {
			t.Errorf("ReservedVMIDReason(%q)=(%v, %q) want (%v, %q)", tc.id, got, reason, tc.want, tc.reason)
		}
	}
}

func TestComposeRoleAgentID(t *testing.T) {
	if got := ComposeRoleAgentID("coder", "plan-1"); got != "coder-plan-1" {
		t.Fatalf("compose with hint: %q", got)
	}
	if got := ComposeRoleAgentID("coder", ""); got != "coder" {
		t.Fatalf("compose without hint: %q", got)
	}
	// Empty roleType plus a hint is the empty-segment form ReservedVMIDReason rejects.
	if got := ComposeRoleAgentID("", "persona-ciso"); got != "-persona-ciso" {
		t.Fatalf("empty role compose: %q", got)
	}
	if reserved, _ := ReservedVMIDReason(ComposeRoleAgentID("", "persona-ciso")); !reserved {
		t.Fatal("empty roleType with a hint must be reserved")
	}
}

func TestCheckRoleAgentIDAllowsCurrentCallers(t *testing.T) {
	// Roles ensure_role actually sends today: PM extractRolesFromText, portal
	// goal, channel fan-out, and `aegis pm goal`. None of these are reserved.
	roles := []string{
		"coder", "tester", "ciso", "security-architect", "architect",
		"efficiency", "user-advocate", "project-manager", "researcher",
	}
	for _, role := range roles {
		id, err := CheckRoleAgentID(role, "plan-1")
		if err != nil {
			t.Errorf("%s: %v", role, err)
		}
		if id != role+"-plan-1" {
			t.Errorf("%s id=%q", role, id)
		}
		if _, err := CheckRoleAgentID(role, ""); err != nil {
			t.Errorf("%s bare: %v", role, err)
		}
	}
	// Paired path does not compose an id.
	if id, err := CheckRoleAgentID("agent", "sess"); err != nil || id != "" {
		t.Fatalf("paired agent: id=%q err=%v", id, err)
	}
	if id, err := CheckRoleAgentID("", "sess"); err != nil || id != "" {
		t.Fatalf("paired empty: id=%q err=%v", id, err)
	}
}

func TestValidateVMID(t *testing.T) {
	// Ids the system actually starts. Channel ids the generators emit are
	// lowercase [a-z0-9-] ("main", "plan-demo", "plan-demo-e2e-llm").
	// chatstore.newID is lowercase hex. Channel ids use ValidateChannelID
	// (same charset, capped so role-channel fits). Uppercase, '_', and '.'
	// are refused, not lowercased.
	session := "0123456789abcdefabcd" // 20 hex bytes, the newID upper bound
	valid := []string{
		"store",
		"network-boundary",
		"web-portal",
		"court-scribe",
		"aegishub",
		"builder",
		"builder-1",
		"builder-sit-1",
		"court-persona-ciso",
		"court-persona-security-architect",
		"court-persona-architect",
		"court-persona-senior-coder",
		"court-persona-tester",
		"court-persona-efficiency",
		"court-persona-user-advocate",
		"project-manager",
		"project-manager-main",
		"project-manager-plan-demo",
		"project-manager-plan-demo-e2e-llm",
		"coder-1",
		"coder-10",
		"coder-plan-1",
		"coder-main",
		"agent-" + session,
		"memory-" + session,
		"agent-temp-agent",
		"memory-temp-agent",
		"memory-sess-1710000000000",
		strings.Repeat("a", 64),
	}
	if got := len("court-persona-security-architect"); got > 64 {
		t.Fatalf("court persona id length %d exceeds cap", got)
	}
	for _, id := range valid {
		if err := ValidateVMID(id); err != nil {
			t.Errorf("ValidateVMID(%q) = %v", id, err)
		}
	}

	invalid := []string{
		"",
		"Coder-1",
		"A",
		"../x",
		"../../../x",
		"..",
		"coder-",
		"foo--bar",
		"ok_Name.1",
		"web-portal.1",
		"chan_1",
		"-leading",
		"1abc",
		"has space",
		strings.Repeat("a", 65),
	}
	for _, id := range invalid {
		err := ValidateVMID(id)
		if !errors.Is(err, ErrInvalidVMID) {
			t.Errorf("ValidateVMID(%q) = %v, want ErrInvalidVMID", id, err)
		}
	}
}

func TestCheckRoleAgentIDAllowlistAndChannels(t *testing.T) {
	id1, err := CheckRoleAgentID("coder", "1")
	if err != nil || id1 != "coder-1" {
		t.Fatalf("coder/1: id=%q err=%v", id1, err)
	}
	id10, err := CheckRoleAgentID("coder", "10")
	if err != nil || id10 != "coder-10" {
		t.Fatalf("coder/10: id=%q err=%v", id10, err)
	}
	if id1 == id10 {
		t.Fatal("coder-1 and coder-10 must be distinct")
	}
	bare, err := CheckRoleAgentID("coder", "")
	if err != nil || bare != "coder" {
		t.Fatalf("coder bare: id=%q err=%v", bare, err)
	}
	pm, err := CheckRoleAgentID("project-manager", "plan-1")
	if err != nil || pm != "project-manager-plan-1" {
		t.Fatalf("pm: id=%q err=%v", pm, err)
	}

	refused := []struct {
		role, channel string
		sentinel      error
	}{
		{"Coder", "", ErrRoleNotAllowed},
		{"Coder", "1", ErrRoleNotAllowed},
		{"coder-1", "", ErrRoleNotAllowed},
		{"../x", "", ErrRoleNotAllowed},
		{"coder", "../../../x", ErrInvalidVMID},
		{"coder", "Plan-1", ErrInvalidVMID},
		{"coder", "chan_1", ErrInvalidVMID},
		{"court-persona-ciso", "", ErrReservedRoleID},
		{"court", "persona-ciso", ErrReservedRoleID},
		{"store", "", ErrReservedRoleID},
		{"network-boundary", "", ErrReservedRoleID},
		{"web-portal", "", ErrReservedRoleID},
		{"court-scribe", "", ErrReservedRoleID},
		{"general", "main", ErrRoleNotAllowed},
		{"sdlc-coder", "main", ErrRoleNotAllowed},
		{"senior-coder", "main", ErrRoleNotAllowed},
		{"analyst", "", ErrRoleNotAllowed},
		{"critic", "", ErrRoleNotAllowed},
		{"agent", "../../../x", ErrInvalidVMID},
		{"", "../../../x", ErrInvalidVMID},
	}
	for _, tc := range refused {
		_, err := CheckRoleAgentID(tc.role, tc.channel)
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("CheckRoleAgentID(%q, %q) = %v, want %v", tc.role, tc.channel, err, tc.sentinel)
		}
	}

	longCh := strings.Repeat("a", 58) // "coder-" + 58 = 64
	if id, err := CheckRoleAgentID("coder", longCh); err != nil || len(id) != 64 {
		t.Fatalf("64-byte id: %q err=%v", id, err)
	}
	if _, err := CheckRoleAgentID("coder", strings.Repeat("a", 59)); !errors.Is(err, ErrInvalidVMID) {
		t.Fatalf("65-byte id: %v", err)
	}
}

func TestHostOnlyVMID(t *testing.T) {
	yes := []string{"hub", "hub-perm-fetch", "hub-perm-fetch-1", "daemon", "daemon-orchestrator", "daemon-internal-1", "aegis-cli-internal", "aegis-cli-internal-1", "channel-facilitator", "channel-facilitator-out-1", "aegis-daemon-temp", "aegis-daemon-temp-x", "daemon-temp-1"}
	no := []string{"hub-perm-fetcher", "daemonfoo", "store", "coder-1", "aegishub", "Daemon"}
	for _, id := range yes {
		if !HostOnlyVMID(id) {
			t.Errorf("HostOnlyVMID(%q) = false", id)
		}
	}
	for _, id := range no {
		if HostOnlyVMID(id) {
			t.Errorf("HostOnlyVMID(%q) = true", id)
		}
	}
}

func TestCheckRoleAgentIDRejectsReserved(t *testing.T) {
	cases := []struct{ role, channel string }{
		{"court-persona-ciso", ""},
		{"court", "persona-ciso"},
		{"court", "ciso"},
		{"store", ""},
		{"network-boundary", ""},
		{"web-portal", "main"},
		{"court-scribe", ""},
		{"daemon", "orchestrator"},
		{"memory", "abc"},
		{"builder", ""},
		{"hub", ""},
		{"aegishub", ""},
		{"channel-facilitator", ""},
		{"aegis-cli-internal", ""},
	}
	for _, tc := range cases {
		_, err := CheckRoleAgentID(tc.role, tc.channel)
		if !errors.Is(err, ErrReservedRoleID) {
			t.Errorf("CheckRoleAgentID(%q, %q) err=%v", tc.role, tc.channel, err)
		}
	}
}

// Every hub host client family, and in particular every family the hub
// serves with its ephemeral RPC loop, is a reserved VM id and host-only on
// the guest bridge.
func TestHostClientFamiliesReservedForVMs(t *testing.T) {
	if len(hubids.EphemeralClientFamilies) == 0 {
		t.Fatal("no ephemeral client families")
	}
	for _, fam := range hubids.EphemeralClientFamilies {
		found := false
		for _, h := range hubids.HostClientFamilies {
			found = found || h == fam
		}
		if !found {
			t.Errorf("ephemeral family %q missing from HostClientFamilies", fam)
		}
	}
	for _, fam := range hubids.HostClientFamilies {
		for _, id := range []string{fam, fam + "-x", fam + "-1"} {
			if reserved, _ := ReservedVMIDReason(id); !reserved {
				t.Errorf("ReservedVMIDReason(%q) = false", id)
			}
			if !HostOnlyVMID(id) {
				t.Errorf("HostOnlyVMID(%q) = false", id)
			}
			if _, err := CheckRoleAgentID(id, ""); !EnsureRoleRefused(err) {
				t.Errorf("CheckRoleAgentID(%q) = %v, want refused", id, err)
			}
		}
	}
	reserved, reason := ReservedVMIDReason("aegis-daemon-temp-x")
	if !reserved || reason != "reserved prefix aegis-daemon-temp" {
		t.Fatalf("ReservedVMIDReason(aegis-daemon-temp-x) = %v, %q", reserved, reason)
	}
	if _, err := CheckRoleAgentID("coder", "aegis-daemon-temp-x"); err != nil {
		t.Fatalf("coder with a channel hint is a normal role id: %v", err)
	}
}
