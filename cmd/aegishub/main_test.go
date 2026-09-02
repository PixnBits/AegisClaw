package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdlayher/vsock"
)

// NOTE (Phase 1.1c): AegisHub now also listens on vsock port 9999 (when available)
// for real Firecracker guest microVMs (Agent Runtime + Memory VM).
// The existing unix-socket roundtrip tests continue to cover the shared handleConnection logic.
// Vsock-specific integration is exercised when running inside actual microVMs (see AGENTS.md + build-microvms).

func waitUnixReady(t *testing.T, sock string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var dialErr error
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", sock, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		dialErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hub not accepting on %s: %v", sock, dialErr)
}

func buildTestBinary(t *testing.T, pkgPath, binaryName string) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	binPath := filepath.Join(t.TempDir(), binaryName)
	buildCmd := exec.Command("go", "build", "-o", binPath, pkgPath)
	buildCmd.Dir = repoRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build %s: %v\n%s", pkgPath, err, output)
	}

	return binPath
}

func TestHubRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "aegishub.sock")

	// Generate keys for clients
	pub1, priv1, _ := ed25519.GenerateKey(rand.Reader)
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	pub1Str := base64.StdEncoding.EncodeToString(pub1)
	pub2Str := base64.StdEncoding.EncodeToString(pub2)

	// Start hub in background
	hubBinary := buildTestBinary(t, "./cmd/aegishub", "aegishub-test")
	cmd := exec.Command(hubBinary, "start")
	// Allow dummy signatures in the test (the test was written for the lenient registration path).
	// Real components will send proper signatures; production Hub rejects dummy unless this env is set.
	// Compute repoRoot for reliable ACL file path (test may exec binary from temp dir)
	wd, _ := os.Getwd()
	repoRootForACL := filepath.Clean(filepath.Join(wd, "..", ".."))
	aclPath := filepath.Join(repoRootForACL, "config", "acls.yaml")
	cmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+sock, "AEGIS_DEV_MODE=1", "AEGIS_ACL_FILE="+aclPath)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer cmd.Process.Kill()

	waitUnixReady(t, sock, 5*time.Second)

	// Connect client1
	conn1, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Failed to connect client1: %v", err)
	}
	defer conn1.Close()

	// Connect client2
	conn2, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Failed to connect client2: %v", err)
	}
	defer conn2.Close()

	// Register client1
	encoder1 := json.NewEncoder(conn1)
	decoder1 := json.NewDecoder(conn1)
	regMsg1 := Message{
		Source:      "client1",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pub1Str},
		Timestamp:   "2026-05-09T19:20:00Z",
		Signature:   "",
	}
	// Sign registration (same pattern the test already uses for data messages)
	data1, _ := json.Marshal(regMsg1)
	sig1 := ed25519.Sign(priv1, data1)
	regMsg1.Signature = base64.StdEncoding.EncodeToString(sig1)
	err = encoder1.Encode(regMsg1)
	if err != nil {
		t.Fatalf("Failed to register client1: %v", err)
	}
	var resp1 map[string]interface{}
	err = decoder1.Decode(&resp1)
	if err != nil {
		t.Fatalf("Failed to decode register response for client1: %v", err)
	}
	if error, ok := resp1["error"]; ok {
		t.Fatalf("Register client1 failed: %s", error)
	}

	// Register client2
	encoder2 := json.NewEncoder(conn2)
	decoder2 := json.NewDecoder(conn2)
	regMsg2 := Message{
		Source:      "client2",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pub2Str},
		Timestamp:   "2026-05-09T19:20:00Z",
		Signature:   "",
	}
	// Sign registration (same pattern the test already uses for data messages)
	data2, _ := json.Marshal(regMsg2)
	sig2 := ed25519.Sign(priv2, data2)
	regMsg2.Signature = base64.StdEncoding.EncodeToString(sig2)
	err = encoder2.Encode(regMsg2)
	if err != nil {
		t.Fatalf("Failed to register client2: %v", err)
	}
	// Consume response
	var resp2 map[string]interface{}
	err = decoder2.Decode(&resp2)
	if err != nil {
		t.Fatalf("Failed to decode register response: %v", err)
	}
	if error, ok := resp2["error"]; ok {
		t.Fatalf("Register client2 failed: %s", error)
	}

	// Send message from client1 to client2
	msg := Message{
		Source:      "client1",
		Destination: "client2",
		Command:     "test",
		Payload:     "hello",
		Timestamp:   "2026-05-09T19:20:00Z",
		Signature:   "",
	}
	// Sign the message
	data, _ := json.Marshal(msg)
	signature := ed25519.Sign(priv1, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)

	err = encoder1.Encode(msg)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Client2 should receive the message
	var received Message
	err = decoder2.Decode(&received)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	if received.Source != "client1" || received.Destination != "client2" || received.Command != "test" {
		t.Errorf("Received wrong message: %+v", received)
	}
}

func TestACLMatch(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "anything", true},
		{"agent", "agent", true},
		{"agent", "memory", false},
		{"memory.*", "memory.get_context", true},
		{"memory.*", "memory.store", true},
		{"memory.*", "memoryfoo", false},
		{"court-persona-*", "court-persona-ciso", true},
		{"court-persona-*", "court-persona-security-architect", true},
		{"court-persona-*", "court-persona", false},
		{"scribe.notify_review", "scribe.notify_review", true},
		{"foo", "foobar", false}, // stricter now
		{"test", "test", true},
	}
	for _, tt := range tests {
		got := aclMatch(tt.pattern, tt.value)
		if got != tt.want {
			t.Errorf("aclMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

func TestCheckACL(t *testing.T) {
	// Save/restore global
	orig := aclRules
	defer func() { aclRules = orig }()

	aclRules = []ACLRule{
		{Source: "agent", Destination: "memory", Commands: []string{"memory.*"}},
		{Source: "agent", Destination: "store", Commands: []string{"proposal.*"}},
		{Source: "court-persona-*", Destination: "court-scribe", Commands: []string{"scribe.submit_vote"}},
		{Source: "coder*", Destination: "network-boundary", Commands: []string{"llm.*"}},
		{Source: "coder*", Destination: "store", Commands: []string{"channel.*"}},
		{Source: "*", Destination: "hub", Commands: []string{"version", "get-version"}},
		{Source: "client1", Destination: "client2", Commands: []string{"test"}},
	}

	cases := []struct {
		src, dst, cmd string
		want          bool
	}{
		{"agent", "memory", "memory.get_context", true},
		{"agent", "memory", "memory.search", true},
		{"agent", "store", "proposal.create", true},
		{"agent", "store", "proposal.get", true},
		{"agent", "store", "skill.list", false},
		{"court-persona-ciso", "court-scribe", "scribe.submit_vote", true},
		{"court-persona-tester", "court-scribe", "scribe.submit_vote", true},
		{"court-persona-foo", "court-scribe", "scribe.notify_review", false},
		{"foo", "hub", "version", true},
		{"client1", "client2", "test", true},
		{"client1", "client2", "other", false},
		{"agent", "memory", "other", false},
		{"coder-another-fresh", "network-boundary", "llm.call", true},
		{"coder-another-fresh", "store", "channel.post", true},
	}
	for _, c := range cases {
		if got := checkACL(c.src, c.dst, c.cmd); got != c.want {
			t.Errorf("checkACL(%q,%q,%q)=%v want %v", c.src, c.dst, c.cmd, got, c.want)
		}
	}
}

func TestVerifyWireSignatureSurvivesPayloadRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Simulates store permission.list after appendAuditForStateChangeIfNeeded wraps grants.
	payload := json.RawMessage(`{"merkle_root":"abc","result":[{"subject":"pm","capability":"channel.post","granted_by":"boot","granted_at":"t"}]}`)
	wire := wireMessage{
		Source: "store", Destination: "daemon-internal", Command: "permission.list",
		Payload: payload, Timestamp: "2026-01-01T00:00:00Z",
	}
	sigBody, _ := json.Marshal(func() wireMessage {
		w := wire
		w.Signature = ""
		return w
	}())
	sig := ed25519.Sign(priv, sigBody)
	wire.Signature = base64.StdEncoding.EncodeToString(sig)

	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var round wireMessage
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatal(err)
	}
	if !verifyWireSignature(round, pub) {
		t.Fatal("verifyWireSignature failed after JSON round-trip")
	}
	var msg Message
	if err := json.Unmarshal(encoded, &msg); err != nil {
		t.Fatal(err)
	}
	if verifySignature(msg, pub) {
		t.Fatal("verifySignature should fail after interface{} payload round-trip (regression guard)")
	}
}

func TestDeliverPendingRPC_DoesNotStealUnrelatedPush(t *testing.T) {
	requester := "project-manager-diag"
	waitCh := registerPendingRPC(requester, "network-boundary", "llm.call")
	defer clearPendingRPC(requester)

	turn := Message{
		Source:      "channel-facilitator-out-1",
		Destination: requester,
		Command:     "channel.turn",
	}
	if deliverPendingRPC(turn) {
		t.Fatal("channel.turn must not complete an in-flight llm.call waiter")
	}

	reply := Message{
		Source:      "network-boundary",
		Destination: requester,
		Command:     "llm.call.response",
		Payload:     map[string]string{"response": "ok"},
	}
	if !deliverPendingRPC(reply) {
		t.Fatal("llm.call.response from network-boundary should complete the waiter")
	}
	select {
	case got := <-waitCh:
		if got.Command != "llm.call.response" {
			t.Fatalf("waiter got %s", got.Command)
		}
	default:
		t.Fatal("waiter did not receive llm.call.response")
	}
}

func TestIsOneWayHubReply_LLMResponse(t *testing.T) {
	if !isOneWayHubReply("llm.call.response") {
		t.Fatal("llm.call.response must be forwarded as a reply, not a new RPC")
	}
	if isOneWayHubReply("channel.turn") {
		t.Fatal("channel.turn is a push RPC, not a one-way reply")
	}
	if !isOneWayHubReply("channel.posted") {
		t.Fatal("channel.posted is a store RPC reply")
	}
}

func TestForwardHubRPC_ChannelTurnDoesNotWaitForDestReply(t *testing.T) {
	destClient, destHub := net.Pipe()
	defer destClient.Close()
	defer destHub.Close()
	encoders := &ComponentEncoders{
		Encoder: json.NewEncoder(destHub),
		Decoder: json.NewDecoder(destHub),
	}
	registeredMutex.Lock()
	registered["pm-busy"] = &RegisteredComponent{ID: "pm-busy", Encoders: encoders}
	registeredMutex.Unlock()
	defer func() {
		registeredMutex.Lock()
		delete(registered, "pm-busy")
		registeredMutex.Unlock()
	}()

	// Guest still reads the turn; it just does not Reply while inside llm.call.
	go func() {
		dec := json.NewDecoder(destClient)
		var got Message
		_ = dec.Decode(&got)
	}()

	start := time.Now()
	reply := forwardHubRPC("channel-facilitator-out-test", Message{
		Source:      "channel-facilitator-out-test",
		Destination: "pm-busy",
		Command:     "channel.turn",
		Payload:     map[string]string{"channel_id": "p5-css"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("channel.turn wait %s; push must return without dest Reply", elapsed)
	}
	if reply.Command == "error" {
		t.Fatalf("channel.turn push error: %v", reply.Payload)
	}
	if reply.Command != "response" {
		t.Fatalf("channel.turn push command %q, want response", reply.Command)
	}
}

func TestIsOneWayHubPush(t *testing.T) {
	if !isOneWayHubPush("channel.turn") {
		t.Fatal("channel.turn must be a one-way push")
	}
	if isOneWayHubPush("llm.call") {
		t.Fatal("llm.call is a blocking RPC")
	}
}

func startGitHub(t *testing.T, identities map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "aegishub.sock")
	identPath := filepath.Join(dir, "git-identities.json")
	b, err := json.Marshal(identities)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	hubBinary := buildTestBinary(t, "./cmd/aegishub", "aegishub-git-test")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	aclPath := filepath.Join(filepath.Clean(filepath.Join(wd, "..", "..")), "config", "acls.yaml")
	cmd := exec.Command(hubBinary, "start")
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "AEGIS_HUB_SOCKET=") ||
			strings.HasPrefix(e, "AEGIS_GIT_IDENTITIES=") ||
			strings.HasPrefix(e, "AEGIS_GIT_CID_KEYS=") ||
			strings.HasPrefix(e, "AEGIS_GIT_ALLOW_UNIX=") ||
			strings.HasPrefix(e, "AEGIS_STORE_GIT_SOCKET=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env,
		"AEGIS_HUB_SOCKET="+sock,
		"AEGIS_GIT_IDENTITIES="+identPath,
		"AEGIS_DEV_MODE=1",
		"AEGIS_ACL_FILE="+aclPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start hub: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	waitUnixReady(t, sock, 5*time.Second)
	return sock
}

func signGitRegister(priv ed25519.PrivateKey, payload map[string]string) Message {
	msg := Message{
		Source:      "git-remote-hub",
		Destination: "hub",
		Command:     "register",
		Payload:     payload,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(msg)
	msg.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	return msg
}

func gitConnectAfterRegister(t *testing.T, sock string, reg Message, url string) string {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(reg); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	reply, err := br.ReadString('\n')
	out := reply
	if err != nil {
		out += err.Error()
	}
	_, _ = fmt.Fprintf(conn, "git-connect git-upload-pack %s\n", url)
	line, err2 := br.ReadString('\n')
	out += line
	if err2 != nil {
		out += err2.Error()
	}
	return out
}

func TestGitConnectUnknownKeyIgnoresPayloadTenant(t *testing.T) {
	_, aPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	unkPub, unkPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sock := startGitHub(t, map[string]string{
		base64.StdEncoding.EncodeToString(aPriv.Public().(ed25519.PublicKey)): "tenant-a",
	})
	reg := signGitRegister(unkPriv, map[string]string{
		"public_key": base64.StdEncoding.EncodeToString(unkPub),
		"version":    "git-remote-hub",
		"tenant":     "tenant-a",
	})
	got := gitConnectAfterRegister(t, sock, reg, "hub::vsock/tenant-a/skill")
	low := strings.ToLower(got)
	if strings.Contains(low, "not your tenant") {
		t.Fatalf("unknown peer deny must not be tenancy needle: %q", got)
	}
	if strings.TrimSpace(got) == "ok" || strings.HasSuffix(strings.TrimSpace(got), "\nok") {
		t.Fatalf("unknown key + payload.tenant must not git-connect: %q", got)
	}
	if strings.Contains(low, "deny store git socket") {
		t.Fatalf("payload.tenant must not grant a session that reaches Store: %q", got)
	}
}

func TestGitConnectUnsignedCannotClaimRosteredKey(t *testing.T) {
	aPub, aPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sock := startGitHub(t, map[string]string{
		base64.StdEncoding.EncodeToString(aPub): "tenant-a",
	})
	payload := map[string]string{
		"public_key": base64.StdEncoding.EncodeToString(aPub),
		"version":    "git-remote-hub",
	}
	cases := []struct {
		name string
		reg  Message
	}{
		{"empty", Message{Source: "git-remote-hub", Destination: "hub", Command: "register", Payload: payload, Timestamp: time.Now().UTC().Format(time.RFC3339)}},
		{"dummy", Message{Source: "git-remote-hub", Destination: "hub", Command: "register", Payload: payload, Timestamp: time.Now().UTC().Format(time.RFC3339), Signature: "dummy"}},
		{"wrong-key", signGitRegister(otherPriv, payload)},
	}
	_ = aPriv
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gitConnectAfterRegister(t, sock, tc.reg, "hub::vsock/tenant-a/skill")
			low := strings.ToLower(got)
			if strings.Contains(low, "not your tenant") {
				t.Fatalf("unverified key deny must not be tenancy needle: %q", got)
			}
			if strings.TrimSpace(got) == "ok" || strings.Contains(low, "\"status\":\"registered\"") && strings.Contains(low, "\nok") {
				t.Fatalf("claimed rostered pubkey without privkey must not get git identity: %q", got)
			}
			if strings.Contains(low, "deny store git socket") {
				t.Fatalf("unverified register must not reach Store: %q", got)
			}
		})
	}
}

func resetCIDLeases() {
	cidLease = sync.Map{}
	cidClosed = sync.Map{}
}

type remoteAddrConn struct {
	net.Conn
	remote net.Addr
}

func (c *remoteAddrConn) RemoteAddr() net.Addr { return c.remote }

func TestParseCIDKeyEncoding(t *testing.T) {
	cid, ok := parseCIDKey("3")
	if !ok || cid != 3 {
		t.Fatalf("decimal 3: cid=%d ok=%v", cid, ok)
	}
	if _, ok := parseCIDKey("cid-3"); ok {
		t.Fatal(`"cid-3" must not parse; CID encoding is decimal uint32`)
	}
}

func TestTenantForGitVsockCIDLease(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)

	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAStr := base64.StdEncoding.EncodeToString(pubA)
	pubBStr := base64.StdEncoding.EncodeToString(pubB)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	cidPath := filepath.Join(dir, "cid-keys.json")
	identJSON, err := json.Marshal(map[string]string{pubAStr: "tenant-a", pubBStr: "tenant-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	const cid uint32 = 42
	cidJSON, err := json.Marshal(map[string]string{
		"42":    pubAStr,
		"cid-3": pubAStr, // must be ignored — only decimal uint32 keys
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cidPath, cidJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	t.Setenv("AEGIS_GIT_CID_KEYS", cidPath)
	t.Setenv("AEGIS_GIT_ALLOW_UNIX", "")
	loadCIDKeys()

	addr := &vsock.Addr{ContextID: cid, Port: 9999}

	got, err := tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("CID leased to A + A's key: tenant=%q err=%v, want tenant-a", got, err)
	}

	got, err = tenantForGit(pubBStr, addr)
	if err == nil || got != "" {
		t.Fatalf("CID leased to A + B's key must not Serve: tenant=%q err=%v", got, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "not your tenant") {
		t.Fatalf("CID key mismatch must not be tenancy needle: %v", err)
	}

	got, err = tenantForGit(pubAStr, &vsock.Addr{ContextID: 3, Port: 9999})
	if err == nil || got != "" {
		t.Fatalf("cid-3 file key must not lease CID 3: tenant=%q err=%v", got, err)
	}

	got, err = tenantForGit(pubAStr, &vsock.Addr{ContextID: 99, Port: 9999})
	if err == nil || got != "" {
		t.Fatalf("unleased CID must not use roster: tenant=%q err=%v", got, err)
	}

	forgetVsockTenant(&remoteAddrConn{remote: addr})
	got, err = tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("after close, reused CID without new lease must deny (leftover file): tenant=%q err=%v", got, err)
	}

	unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
	got, err = tenantForGit(pubAStr, unixAddr)
	if err == nil || got != "" {
		t.Fatalf("unix without AEGIS_GIT_ALLOW_UNIX=1 must not skip CID: tenant=%q err=%v", got, err)
	}

	t.Setenv("AEGIS_GIT_ALLOW_UNIX", "1")
	got, err = tenantForGit(pubAStr, unixAddr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("unix ALLOW_UNIX pubA: tenant=%q err=%v, want tenant-a", got, err)
	}
	got, err = tenantForGit(pubBStr, unixAddr)
	if err != nil || got != "tenant-b" {
		t.Fatalf("unix ALLOW_UNIX pubB: tenant=%q err=%v, want tenant-b", got, err)
	}

	t.Setenv("AEGIS_GIT_IDENTITIES", filepath.Join(dir, "missing-identities.json"))
	got, err = tenantForGit(pubAStr, unixAddr)
	if err == nil || got != "" {
		t.Fatalf("identities[pub] miss must not Serve: tenant=%q err=%v", got, err)
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "not your tenant") {
		t.Fatalf("identity miss must not be tenancy needle: %v", err)
	}
}

func TestGitConnectUnixDeniedWithoutAllowUnix(t *testing.T) {
	aPub, aPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sock := startGitHub(t, map[string]string{
		base64.StdEncoding.EncodeToString(aPub): "tenant-a",
	})
	reg := signGitRegister(aPriv, map[string]string{
		"public_key": base64.StdEncoding.EncodeToString(aPub),
		"version":    "git-remote-hub",
	})
	got := gitConnectAfterRegister(t, sock, reg, "hub::vsock/tenant-a/skill")
	low := strings.ToLower(got)
	if strings.Contains(low, "not your tenant") {
		t.Fatalf("unix without ALLOW_UNIX deny must not be tenancy needle: %q", got)
	}
	if strings.TrimSpace(got) == "ok" || strings.HasSuffix(strings.TrimSpace(got), "\nok") {
		t.Fatalf("stolen privkey + unix must not Serve: %q", got)
	}
	if strings.Contains(low, "deny store git socket") {
		t.Fatalf("unix without ALLOW_UNIX must not reach Store: %q", got)
	}
}
