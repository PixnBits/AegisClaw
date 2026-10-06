package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// handshakeOn runs handleConnection on wrap(pipe end) and returns the first reply.
func handshakeOn(t *testing.T, wrap func(net.Conn) net.Conn, reg Message) map[string]interface{} {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(wrap(hub), &sync.Map{})
	}()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		resp = map[string]interface{}{"decode_err": err.Error()}
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleConnection did not return")
	}
	return resp
}

func replyError(resp map[string]interface{}) string {
	s, _ := resp["error"].(string)
	return s
}

func TestVsockRegistrationRefusesHostAndBaseIDs(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	snapshotHubRegistry(t)
	priv, pubStr := newTestPub(t)
	ids := []string{
		"aegis-daemon-temp", "aegis-daemon-temp-1", "daemon-temp-1",
		"daemon", "daemon-internal", "daemon-internal-3", "daemon-internalx", "daemon-orchestrator",
		"aegis-cli-internal", "aegis-cli-internal-5", "aegis-cli-internalx",
		"channel-facilitator", "store", "network-boundary", "web-portal", "aegishub", "hub",
	}
	for i, id := range ids {
		addr := &vsock.Addr{ContextID: uint32(100 + i), Port: 9999}
		resp := guestVsockHandshake(t, addr, signRegisterSource(priv, id, pubStr))
		if !strings.Contains(replyError(resp), "ERR_RESERVED_ID") {
			t.Errorf("vsock register %q = %#v, want ERR_RESERVED_ID", id, resp)
		}
		if ok, where := hubIDRegistered(id, &sync.Map{}); ok {
			t.Errorf("%q left in %s", id, where)
		}
	}
	// Positive control: a guest id still registers over vsock.
	resp := guestVsockHandshake(t, &vsock.Addr{ContextID: 99, Port: 9999}, signRegisterSource(priv, "coder-1", pubStr))
	if resp["status"] != "registered" {
		t.Fatalf("coder-1 on vsock: %#v", resp)
	}
}

func TestVsockNonGuestCIDRefused(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	snapshotHubRegistry(t)
	priv, pubStr := newTestPub(t)
	for _, cid := range []uint32{0, 1, 2, ^uint32(0)} {
		resp := guestVsockHandshake(t, &vsock.Addr{ContextID: cid, Port: 9999}, signRegisterSource(priv, "coder-1", pubStr))
		if !strings.Contains(replyError(resp), "ERR_UNAUTHORIZED_PEER") {
			t.Errorf("CID %d register = %#v, want ERR_UNAUTHORIZED_PEER", cid, resp)
		}
	}
	if ok, where := hubIDRegistered("coder-1", &sync.Map{}); ok {
		t.Fatalf("coder-1 registered from a non-guest CID (%s)", where)
	}
	for _, cid := range []uint32{0, 1, 2, ^uint32(0)} {
		if isGuestCID(cid) {
			t.Errorf("isGuestCID(%d) = true", cid)
		}
	}
	if !isGuestCID(3) {
		t.Error("isGuestCID(3) = false")
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
	for _, id := range []string{"daemon-internal", "daemon-internal-1", "aegis-cli-internal-77", "aegis-daemon-temp-2"} {
		resp := guestVsockHandshake(t, unix, signRegisterSource(priv, id, pubStr))
		if resp["status"] != "registered" {
			t.Errorf("non-vsock register %q = %#v, want registered", id, resp)
		}
	}
}

func TestAdmitVsockPeer(t *testing.T) {
	allow := func() (map[uint32]string, error) { return map[uint32]string{42: "coder-1", 43: "agent-s1"}, nil }
	broken := func() (map[uint32]string, error) { return nil, errors.New("no file") }
	cases := []struct {
		addr   net.Addr
		load   func() (map[uint32]string, error)
		wantID string
	}{
		{&vsock.Addr{ContextID: 42}, allow, "coder-1"},
		{&vsock.Addr{ContextID: 43}, allow, "agent-s1"},
		{&vsock.Addr{ContextID: 44}, allow, ""},
		{&vsock.Addr{ContextID: 1}, allow, ""},
		{&vsock.Addr{ContextID: 2}, allow, ""},
		{&vsock.Addr{ContextID: 0}, allow, ""},
		{&vsock.Addr{ContextID: ^uint32(0)}, allow, ""},
		{&vsock.Addr{ContextID: 42}, broken, ""},
		{&net.UnixAddr{Name: "x", Net: "unix"}, allow, ""},
		{nil, allow, ""},
	}
	for i, tc := range cases {
		id, err := admitVsockPeer(tc.addr, tc.load)
		if tc.wantID == "" {
			if err == nil {
				t.Errorf("case %d (%v): admitted as %q, want refused", i, tc.addr, id)
			}
			continue
		}
		if err != nil || id != tc.wantID {
			t.Errorf("case %d (%v): got %q, %v; want %q", i, tc.addr, id, err, tc.wantID)
		}
	}
	// CID 1 is refused even when the allowlist names it.
	withLocal := func() (map[uint32]string, error) { return map[uint32]string{1: "coder-1", 2: "coder-2"}, nil }
	for _, cid := range []uint32{1, 2} {
		if _, err := admitVsockPeer(&vsock.Addr{ContextID: cid}, withLocal); err == nil {
			t.Errorf("CID %d admitted from the allowlist", cid)
		}
	}
}

func TestLoadVsockCIDAllowlist(t *testing.T) {
	t.Setenv(hubVsockCIDAllowlistEnv, "")
	if _, err := loadVsockCIDAllowlist(); err == nil {
		t.Fatal("unset allowlist env must fail closed")
	}
	dir := t.TempDir()
	t.Setenv(hubVsockCIDAllowlistEnv, filepath.Join(dir, "missing.json"))
	if _, err := loadVsockCIDAllowlist(); err == nil {
		t.Fatal("missing allowlist file must fail closed")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(hubVsockCIDAllowlistEnv, bad)
	if _, err := loadVsockCIDAllowlist(); err == nil {
		t.Fatal("bad allowlist JSON must fail closed")
	}
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"42":"coder-1"," 43 ":"agent-s1","x":"bad","44":" "}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(hubVsockCIDAllowlistEnv, good)
	m, err := loadVsockCIDAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m[42] != "coder-1" || m[43] != "agent-s1" {
		t.Fatalf("allowlist = %#v", m)
	}
}

func TestVsockBoundIDMustMatch(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	snapshotHubRegistry(t)
	priv, pubStr := newTestPub(t)
	bound := func(c net.Conn) net.Conn {
		return &boundVsockConn{Conn: &remoteAddrConn{Conn: c, remote: &vsock.Addr{ContextID: 42, Port: 9999}}, vmID: "coder-1"}
	}
	if resp := handshakeOn(t, bound, signRegisterSource(priv, "coder-2", pubStr)); !strings.Contains(replyError(resp), "ERR_UNBOUND_ID") {
		t.Fatalf("coder-2 on a CID bound to coder-1 = %#v, want ERR_UNBOUND_ID", resp)
	}
	if resp := handshakeOn(t, bound, signRegisterSource(priv, "coder-1", pubStr)); resp["status"] != "registered" {
		t.Fatalf("coder-1 on its own CID = %#v, want registered", resp)
	}
	// The bound id is still subject to the reserved-id rules.
	boundStore := func(c net.Conn) net.Conn {
		return &boundVsockConn{Conn: &remoteAddrConn{Conn: c, remote: &vsock.Addr{ContextID: 43, Port: 9999}}, vmID: "store"}
	}
	if resp := handshakeOn(t, boundStore, signRegisterSource(priv, "store", pubStr)); !strings.Contains(replyError(resp), "ERR_RESERVED_ID") {
		t.Fatalf("store on vsock even when bound = %#v, want ERR_RESERVED_ID", resp)
	}
}

func TestHubVsockListenDefaultOff(t *testing.T) {
	for _, v := range []string{"", "0", "true", "yes", "on", " 2"} {
		t.Setenv(hubVsockListenEnv, v)
		if hubVsockListenEnabled() {
			t.Errorf("%s=%q enabled the vsock listener", hubVsockListenEnv, v)
		}
	}
	t.Setenv(hubVsockListenEnv, "1")
	if !hubVsockListenEnabled() {
		t.Error("vsock listener not enabled by =1")
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

func TestMaybeStartVsockListener(t *testing.T) {
	started := make(chan struct{}, 1)
	start := func(*sync.Map) { started <- struct{}{} }
	t.Setenv(hubVsockListenEnv, "")
	if maybeStartVsockListener(&sync.Map{}, start) {
		t.Fatal("listener started without opt-in")
	}
	select {
	case <-started:
		t.Fatal("start ran without opt-in")
	case <-time.After(50 * time.Millisecond):
	}
	t.Setenv(hubVsockListenEnv, "1")
	if !maybeStartVsockListener(&sync.Map{}, start) {
		t.Fatal("listener not started with opt-in")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("start did not run with opt-in")
	}
}

type closeRecorder struct {
	net.Conn
	remote net.Addr
	closed bool
}

func (c *closeRecorder) RemoteAddr() net.Addr { return c.remote }
func (c *closeRecorder) Close() error         { c.closed = true; return nil }

func TestServeVsockConnAdmitsOnlyAllowlistedGuests(t *testing.T) {
	allow := func() (map[uint32]string, error) { return map[uint32]string{42: "coder-1", 1: "coder-9"}, nil }
	for _, cid := range []uint32{1, 2, 77} {
		c := &closeRecorder{remote: &vsock.Addr{ContextID: cid, Port: 9999}}
		called := false
		serveVsockConn(c, &sync.Map{}, allow, func(net.Conn, *sync.Map) { called = true })
		if called || !c.closed {
			t.Errorf("CID %d: handled=%v closed=%v, want refused and closed", cid, called, c.closed)
		}
	}
	c := &closeRecorder{remote: &vsock.Addr{ContextID: 42, Port: 9999}}
	var got net.Conn
	serveVsockConn(c, &sync.Map{}, allow, func(conn net.Conn, _ *sync.Map) { got = conn })
	if id, ok := vsockBoundID(got); !ok || id != "coder-1" || c.closed {
		t.Fatalf("CID 42: bound=%q ok=%v closed=%v", id, ok, c.closed)
	}
}
