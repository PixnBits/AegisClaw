package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"AegisClaw/internal/hubids"
	"AegisClaw/internal/hublease"

	"github.com/mdlayher/vsock"
)

func newTestPub(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

func replyError(resp map[string]interface{}) string {
	s, _ := resp["error"].(string)
	return s
}

func TestVsockHubPeersRefused(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	snapshotHubRegistry(t)
	priv, pubStr := newTestPub(t)
	ids := []string{
		"aegis-daemon-temp", "aegis-daemon-temp-x", "daemon-temp-1",
		"daemon", "daemon-internal", "daemon-internal-3", "daemon-internalx",
		"aegis-cli-internal-5", "aegis-cli-internalx",
		"store", "network-boundary", "web-portal", "coder-1", "git-remote-hub",
	}
	for _, cid := range []uint32{0, 1, 2, 42, ^uint32(0)} {
		for _, id := range ids {
			resp := guestVsockHandshake(t, &vsock.Addr{ContextID: cid, Port: 9999}, signRegisterSource(priv, id, pubStr))
			if !strings.Contains(replyError(resp), "ERR_UNAUTHORIZED_PEER") {
				t.Errorf("vsock CID %d register %q = %#v, want ERR_UNAUTHORIZED_PEER", cid, id, resp)
			}
			if ok, where := hubIDRegistered(id, &sync.Map{}); ok {
				t.Errorf("%q left in %s", id, where)
			}
		}
		if _, ok := hublease.LoadLease(cid); ok {
			t.Errorf("vsock CID %d filled a lease", cid)
		}
	}
}

func TestHostIDLookAlikesRefusedOnEveryTransport(t *testing.T) {
	snapshotHubRegistry(t)
	priv, pubStr := newTestPub(t)
	unix := &net.UnixAddr{Name: "@test", Net: "unix"}
	for _, id := range []string{
		"daemon-internalx", "daemon-internal-", "daemon-internal_1", "daemon-internal.1",
		"aegis-cli-internalx", "aegis-cli-internal-", "aegis-daemon-tempx",
	} {
		resp := guestVsockHandshake(t, unix, signRegisterSource(priv, id, pubStr))
		if !strings.Contains(replyError(resp), "ERR_RESERVED_ID") {
			t.Errorf("non-vsock register %q = %#v, want ERR_RESERVED_ID", id, resp)
		}
	}
	// The real host ids still register off vsock.
	for _, id := range []string{"daemon-internal", "daemon-internal-1", "aegis-cli-internal-77", "aegis-daemon-temp-2", "daemonset-1"} {
		resp := guestVsockHandshake(t, unix, signRegisterSource(priv, id, pubStr))
		if resp["status"] != "registered" {
			t.Errorf("non-vsock register %q = %#v, want registered", id, resp)
		}
	}
}

// aclSamples turns every source, destination and command pattern in rules
// into concrete values that the pattern matches.
func aclSamples(rules []ACLRule) (ids, cmds []string) {
	seenID, seenCmd := map[string]bool{}, map[string]bool{}
	addID := func(p string) {
		v := p
		switch {
		case p == "*":
			v = "some-component"
		case strings.HasSuffix(p, "*"):
			v = strings.TrimSuffix(p, "*")
			if !strings.HasSuffix(v, "-") && !strings.HasSuffix(v, ".") {
				v += "-"
			}
			v += "s1"
		}
		if !seenID[v] {
			seenID[v] = true
			ids = append(ids, v)
		}
	}
	addCmd := func(p string) {
		v := p
		switch {
		case p == "*":
			v = "any.command"
		case strings.HasSuffix(p, "*"):
			v = strings.TrimSuffix(p, "*") + "x"
		}
		if !seenCmd[v] {
			seenCmd[v] = true
			cmds = append(cmds, v)
		}
	}
	for _, r := range rules {
		addID(r.Source)
		addID(r.Destination)
		for _, c := range r.Commands {
			addCmd(c)
		}
	}
	return ids, cmds
}

// The daemon's own ids keep exactly the grants the old "daemon-internal*"
// source/destination patterns gave them, and malformed look-alikes get no
// more than an unknown component.
func TestRepoACLDaemonInternalExactSources(t *testing.T) {
	loadRepoACL(t)
	current := aclRules
	for _, r := range current {
		if r.Source == "daemon-internal*" || r.Destination == "daemon-internal*" {
			t.Errorf("rule %s -> %s still uses the daemon-internal* pattern", r.Source, r.Destination)
		}
	}
	legacy := make([]ACLRule, len(current))
	for i, r := range current {
		legacy[i] = r
		if r.Source == "daemon-internal" {
			legacy[i].Source = "daemon-internal*"
		}
		if r.Destination == "daemon-internal" {
			legacy[i].Destination = "daemon-internal*"
		}
	}
	ids, cmds := aclSamples(current)
	check := func(rules []ACLRule, src, dst, cmd string) bool {
		aclRules = rules
		defer func() { aclRules = current }()
		return checkACL(src, dst, cmd)
	}
	daemonIDs := []string{"daemon-internal", "daemon-internal-1", "daemon-internal-42"}
	pairs := 0
	for _, d := range daemonIDs {
		for _, other := range ids {
			for _, cmd := range cmds {
				pairs++
				if got, want := check(current, d, other, cmd), check(legacy, d, other, cmd); got != want {
					t.Errorf("%s -> %s %s = %v, was %v", d, other, cmd, got, want)
				}
				if got, want := check(current, other, d, cmd), check(legacy, other, d, cmd); got != want {
					t.Errorf("%s -> %s %s = %v, was %v", other, d, cmd, got, want)
				}
			}
		}
	}
	if pairs < 1000 {
		t.Fatalf("only %d pairs sampled", pairs)
	}
	// Positive controls from the real file.
	for _, d := range daemonIDs {
		for _, cmd := range []string{"channel.list", "timer.list", "sessions.list", "llm.usage.summary"} {
			if !checkACL(d, "store", cmd) {
				t.Errorf("%s -> store %s = false, want allow", d, cmd)
			}
		}
		if !checkACL("store", d, "channel.list") {
			t.Errorf("store -> %s channel.list = false, want allow", d)
		}
	}
	for _, look := range []string{"daemon-internalx", "daemon-internal_1", "daemon-internalX-1"} {
		for _, other := range ids {
			for _, cmd := range cmds {
				if checkACL(look, other, cmd) && !checkACL("unknown-component", other, cmd) {
					t.Errorf("%s -> %s %s allowed beyond an unknown component", look, other, cmd)
				}
				if checkACL(other, look, cmd) && !checkACL(other, "unknown-component", cmd) {
					t.Errorf("%s -> %s %s allowed beyond an unknown component", other, look, cmd)
				}
			}
		}
	}
}

func TestTenantForGitHasNoVsockPath(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	_, pubStr := newTestPub(t)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	if err := os.WriteFile(identPath, []byte(`{"`+pubStr+`":"tenant-a"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	hublease.StoreLeaseIfAbsentOrSame(42, pubStr)
	if got, err := tenantForGit(pubStr, &vsock.Addr{ContextID: 42, Port: 9999}); err == nil || got != "" {
		t.Fatalf("tenantForGit over vsock = %q, %v; want ERR_UNKNOWN_PEER", got, err)
	}
}

// The hub's ephemeral RPC set is exactly the shared hubids list, so the
// guest bridge and VM id checks reserve the same ids the hub serves that way.
func TestIsEphemeralHubClientUsesSharedList(t *testing.T) {
	for _, fam := range hubids.EphemeralClientFamilies {
		for _, id := range []string{fam, fam + "-1", fam + "-x"} {
			if !isEphemeralHubClient(id) {
				t.Errorf("isEphemeralHubClient(%q) = false", id)
			}
			if reason, ok := reservedIDReason(id, true); !ok {
				t.Errorf("reservedIDReason(%q, vm) = %q, false", id, reason)
			}
		}
		if isEphemeralHubClient(fam + "x") {
			t.Errorf("isEphemeralHubClient(%q) = true", fam+"x")
		}
	}
	for _, id := range []string{"daemon", "daemon-internal-1", "coder-1", "store"} {
		if isEphemeralHubClient(id) {
			t.Errorf("isEphemeralHubClient(%q) = true", id)
		}
	}
}
