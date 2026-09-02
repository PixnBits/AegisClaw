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

	"AegisClaw/internal/hublease"

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
	hublease.Reset()
}

type remoteAddrConn struct {
	net.Conn
	remote net.Addr
}

func (c *remoteAddrConn) RemoteAddr() net.Addr { return c.remote }

func TestLeasePubForCIDMemoryOnlyNoFileMissIngest(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)

	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAStr := base64.StdEncoding.EncodeToString(pubA)
	dir := t.TempDir()
	cidPath := filepath.Join(dir, "cid-keys.json")
	identPath := filepath.Join(dir, "git-identities.json")
	cidJSON, err := json.Marshal(map[string]string{"42": pubAStr})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cidPath, cidJSON, 0600); err != nil {
		t.Fatal(err)
	}
	identJSON, err := json.Marshal(map[string]string{pubAStr: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_CID_KEYS", cidPath)
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	// Intentionally do not call loadCIDKeys: boot ingest is startHub only.

	const cid uint32 = 42
	if pub, ok := leasePubForCID(cid); ok || pub != "" {
		t.Fatalf("file row must not ingest on leasePubForCID miss: pub=%q ok=%v", pub, ok)
	}
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	got, err := tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("file row must not fail-open tenantForGit on miss: tenant=%q err=%v", got, err)
	}

	storeCIDLease(cid, pubAStr) // handshake Store fills memory
	pub, ok := leasePubForCID(cid)
	if !ok || pub != pubAStr {
		t.Fatalf("after handshake storeCIDLease, leasePubForCID must hit: pub=%q ok=%v", pub, ok)
	}
	got, err = tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("after handshake storeCIDLease, tenantForGit must hit: tenant=%q err=%v", got, err)
	}
}

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

	addr := &vsock.Addr{ContextID: cid, Port: 9999}

	got, err := tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("file row must not fail-open lease on miss: tenant=%q err=%v", got, err)
	}
	storeCIDLease(cid, pubAStr) // Handshake/StartVM StoreLease fills the map
	got, err = tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("after StoreLease, CID leased to A: tenant=%q err=%v, want tenant-a", got, err)
	}

	got, err = tenantForGit(pubBStr, addr)
	if err == nil || got != "" || err.Error() != "ERR_UNKNOWN_PEER" {
		t.Fatalf("CID leased to A + B's key: tenant=%q err=%v, want ERR_UNKNOWN_PEER", got, err)
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

	t.Setenv("AEGIS_GIT_IDENTITIES", filepath.Join(dir, "missing-identities.json"))
	got, err = tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("identities[pub] miss must not Serve: tenant=%q err=%v", got, err)
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "not your tenant") {
		t.Fatalf("identity miss must not be tenancy needle: %v", err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)

	left, err := os.ReadFile(cidPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(left), pubAStr) {
		t.Fatalf("file must still contain leftover CID row for A: %s", left)
	}
	forgetVsockTenant(&remoteAddrConn{remote: addr})
	got, err = tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("after helper close, same CID+A must still be tenant-a (file leftover OK): tenant=%q err=%v", got, err)
	}

	daemonUnleaseCID(cid, pubAStr)
	got, err = tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("after daemonUnleaseCID, leftover file same pub must deny: tenant=%q err=%v", got, err)
	}

	over, err := json.Marshal(map[string]string{"42": pubBStr})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cidPath, over, 0600); err != nil {
		t.Fatal(err)
	}
	loadCIDKeys()
	got, err = tenantForGit(pubBStr, addr)
	if err != nil || got != "tenant-b" {
		t.Fatalf("overwrite leftover with new pub then load: tenant=%q err=%v, want tenant-b", got, err)
	}

	if !unixGitAllowed() {
		unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
		got, err = tenantForGit(pubAStr, unixAddr)
		if err == nil || got != "" {
			t.Fatalf("unix git deny must not skip CID: tenant=%q err=%v", got, err)
		}
	}
}
func TestGitConnectUnixDeniedInProduction(t *testing.T) {
	if unixGitAllowed() {
		t.Skip("unix git allowed under -tags testunixgit")
	}
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
		t.Fatalf("unix git-connect deny must not be tenancy needle: %q", got)
	}
	if strings.TrimSpace(got) == "ok" || strings.HasSuffix(strings.TrimSpace(got), "\nok") {
		t.Fatalf("stolen privkey + unix must not Serve: %q", got)
	}
	if strings.Contains(low, "deny store git socket") {
		t.Fatalf("unix git-connect must not reach Store: %q", got)
	}
}

func gitConnectVsock(t *testing.T, addr net.Addr, priv ed25519.PrivateKey, pub, url string) string {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(&remoteAddrConn{Conn: hub, remote: addr}, &sync.Map{})
	}()
	reg := signGitRegister(priv, map[string]string{
		"public_key": pub,
		"version":    "git-remote-hub",
	})
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(client)
	reply, err := br.ReadString('\n')
	out := reply
	if err != nil {
		out += err.Error()
	}
	_, _ = fmt.Fprintf(client, "git-connect git-upload-pack %s\n", url)
	line, err2 := br.ReadString('\n')
	out += line
	if err2 != nil {
		out += err2.Error()
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("git-connect handleConnection did not return")
	}
	return out
}

func gitConnectServed(got string) bool {
	low := strings.ToLower(got)
	if strings.Contains(low, "err_unknown_peer") {
		return false
	}
	if strings.Contains(got, `"status":"registered"`) {
		return true
	}
	trim := strings.TrimSpace(got)
	return trim == "ok" || strings.HasSuffix(trim, "\nok") || strings.Contains(low, "deny store git socket")
}

func startVMVsockSession(t *testing.T, addr net.Addr, pub string) (net.Conn, <-chan struct{}) {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(&remoteAddrConn{Conn: hub, remote: addr}, &sync.Map{})
	}()
	reg := Message{
		Source:      "guest-vm",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pub, "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("VM register decode: %v", err)
	}
	if e, ok := resp["error"]; ok {
		t.Fatalf("VM register error: %v", e)
	}
	_ = client.SetDeadline(time.Time{})
	return client, done
}

func TestVMSessionCIDLease(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)

	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
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
	cidJSON, err := json.Marshal(map[string]string{"42": pubAStr})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cidPath, cidJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	t.Setenv("AEGIS_GIT_CID_KEYS", cidPath)

	addr := &vsock.Addr{ContextID: 42, Port: 9999}
	vmClient, vmDone := startVMVsockSession(t, addr, pubAStr)

	gotA := gitConnectVsock(t, addr, privA, pubAStr, "hub::vsock/tenant-a/skill")
	if !gitConnectServed(gotA) {
		t.Fatalf("VM session CID→A; git-connect A want Serve, got %q", gotA)
	}
	gotB := gitConnectVsock(t, addr, privB, pubBStr, "hub::vsock/tenant-b/skill")
	if gitConnectServed(gotB) || !strings.Contains(gotB, "ERR_UNKNOWN_PEER") {
		t.Fatalf("git-connect B on CID leased to A want ERR_UNKNOWN_PEER, got %q", gotB)
	}
	if strings.Contains(strings.ToLower(gotB), "not your tenant") {
		t.Fatalf("CID mismatch must not be tenancy needle: %q", gotB)
	}

	gotA2 := gitConnectVsock(t, addr, privA, pubAStr, "hub::vsock/tenant-a/skill")
	if !gitConnectServed(gotA2) {
		t.Fatalf("git-connect close must not unlease; second git-connect A got %q", gotA2)
	}

	_ = vmClient.Close()
	select {
	case <-vmDone:
	case <-time.After(3 * time.Second):
		t.Fatal("VM session handleConnection did not return")
	}
	left, err := os.ReadFile(cidPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(left), pubAStr) {
		t.Fatalf("leftover file must still contain same pub: %s", left)
	}
	got, err := tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("after VM hub session close (no daemonUnlease), same CID+A still tenant-a: tenant=%q err=%v", got, err)
	}
	daemonUnleaseCID(42, pubAStr)
	got, err = tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("after daemonUnleaseCID, leftover file same pub must deny: tenant=%q err=%v", got, err)
	}
}

func TestDaemonMayUnleaseCID(t *testing.T) {
	dummy := Message{Signature: "dummy"}
	var wire wireMessage
	if !daemonMayUnleaseCID("daemon", wire, dummy) {
		t.Fatal("assigned_id daemon must allow cid.unlease")
	}
	deny := []string{"git-remote-hub", "guest-vm", "agent-1", "aegis-cli-internal", "store", "daemon-internal", "daemon-temp-1", "aegis-daemon-temp"}
	for _, id := range deny {
		if daemonMayUnleaseCID(id, wire, dummy) {
			t.Fatalf("dummy sig must not allow cid.unlease from %q", id)
		}
	}
}

func sendCIDUnleaseRPC(t *testing.T, remote net.Addr, source, victimPub string) map[string]interface{} {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(&remoteAddrConn{Conn: hub, remote: remote}, &sync.Map{})
	}()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reg := Message{
		Source:      source,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": base64.StdEncoding.EncodeToString(pubKey), "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Signature:   "dummy",
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(client)
	var resp map[string]interface{}
	if err := dec.Decode(&resp); err != nil {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		return map[string]interface{}{"decode_err": err.Error()}
	}
	if _, hasErr := resp["error"]; hasErr {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		return resp
	}
	msg := Message{
		Source:      source,
		Destination: "hub",
		Command:     "cid.unlease",
		Payload:     map[string]interface{}{"cid": uint32(42), "public_key": victimPub},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Signature:   "dummy",
	}
	if err := json.NewEncoder(client).Encode(msg); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		out = map[string]interface{}{"decode_err": err.Error()}
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleConnection did not return")
	}
	return out
}

func TestCIDUnleaseDaemonOnlyAndCAS(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")

	const cid uint32 = 42
	hublease.StoreLease(cid, "pub-a")

	unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
	// Different CID so vsock handshake StoreLease does not overwrite victim 42.
	guestAddr := &vsock.Addr{ContextID: 99, Port: 9999}

	sendCIDUnleaseRPC(t, guestAddr, "guest-vm", "pub-a")
	if leased, ok := hublease.LoadLease(cid); !ok || leased != "pub-a" {
		t.Fatalf("guest must not unlease victim CID: leased=%q ok=%v", leased, ok)
	}

	sendCIDUnleaseRPC(t, unixAddr, "store", "pub-a")
	if leased, ok := hublease.LoadLease(cid); !ok || leased != "pub-a" {
		t.Fatalf("store unix source must not unlease: leased=%q ok=%v", leased, ok)
	}

	sendCIDUnleaseRPC(t, unixAddr, "daemon-temp-1", "pub-a")
	if leased, ok := hublease.LoadLease(cid); !ok || leased != "pub-a" {
		t.Fatalf("daemon-temp-* must not unlease: leased=%q ok=%v", leased, ok)
	}

	sendCIDUnleaseRPC(t, unixAddr, "aegis-daemon-temp-3", "pub-a")
	if leased, ok := hublease.LoadLease(cid); !ok || leased != "pub-a" {
		t.Fatalf("aegis-daemon-temp-* must not unlease: leased=%q ok=%v", leased, ok)
	}

	sendCIDUnleaseRPC(t, unixAddr, "aegis-cli-internal", "pub-a")
	if leased, ok := hublease.LoadLease(cid); !ok || leased != "pub-a" {
		t.Fatalf("aegis-cli-internal must not unlease: leased=%q ok=%v", leased, ok)
	}

	sendCIDUnleaseRPC(t, unixAddr, "daemon", "pub-b")
	if leased, ok := hublease.LoadLease(cid); !ok || leased != "pub-a" {
		t.Fatalf("CAS mismatch must not unlease: leased=%q ok=%v", leased, ok)
	}

	got := sendCIDUnleaseRPC(t, unixAddr, "daemon", "pub-a")
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatalf("daemon CAS unlease must drop lease; reply=%#v", got)
	}
}

func TestCIDUnleaseDaemonRPCDeletesFileRow(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")
	dir := t.TempDir()
	cidPath := filepath.Join(dir, "cid-keys.json")
	if err := os.WriteFile(cidPath, []byte(`{"42":"pub-a","7":"keep"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_CID_KEYS", cidPath)
	loadCIDKeys()
	unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
	got := sendCIDUnleaseRPC(t, unixAddr, "daemon", "pub-a")
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatalf("StopVM-shaped daemon RPC must unlease; reply=%#v", got)
	}
	b, err := os.ReadFile(cidPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["42"]; ok {
		t.Fatalf("file row must be gone after daemon cid.unlease: %s", b)
	}
	if m["7"] != "keep" {
		t.Fatalf("other CID rows must remain: %s", b)
	}

	hublease.Reset()
	loadCIDKeys()
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("leftover file after CAS+file delete must not re-lease on Hub restart/load")
	}
	if got, ok := hublease.LoadLease(7); !ok || got != "keep" {
		t.Fatalf("other CID must still load after restart: got %q ok=%v", got, ok)
	}
}

func sendCIDLeaseRPC(t *testing.T, remote net.Addr, source, pub string) map[string]interface{} {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(&remoteAddrConn{Conn: hub, remote: remote}, &sync.Map{})
	}()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reg := Message{
		Source:      source,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": base64.StdEncoding.EncodeToString(pubKey), "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Signature:   "dummy",
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(client)
	var resp map[string]interface{}
	if err := dec.Decode(&resp); err != nil {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		return map[string]interface{}{"decode_err": err.Error()}
	}
	if _, hasErr := resp["error"]; hasErr {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		return resp
	}
	msg := Message{
		Source:      source,
		Destination: "hub",
		Command:     "cid.lease",
		Payload:     map[string]interface{}{"cid": uint32(7), "public_key": pub},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Signature:   "dummy",
	}
	if err := json.NewEncoder(client).Encode(msg); err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := dec.Decode(&out); err != nil {
		out = map[string]interface{}{"decode_err": err.Error()}
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleConnection did not return")
	}
	return out
}

func TestCIDLeaseDaemonOnly(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")

	unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
	guestAddr := &vsock.Addr{ContextID: 99, Port: 9999}

	sendCIDLeaseRPC(t, guestAddr, "guest-vm", "pub-a")
	if _, ok := hublease.LoadLease(7); ok {
		t.Fatal("guest must not cid.lease")
	}
	sendCIDLeaseRPC(t, unixAddr, "store", "pub-a")
	if _, ok := hublease.LoadLease(7); ok {
		t.Fatal("store must not cid.lease")
	}
	sendCIDLeaseRPC(t, unixAddr, "daemon-temp-1", "pub-a")
	if _, ok := hublease.LoadLease(7); ok {
		t.Fatal("daemon-temp glob must not cid.lease")
	}
	got := sendCIDLeaseRPC(t, unixAddr, "daemon", "pub-a")
	leased, ok := hublease.LoadLease(7)
	if !ok || leased != "pub-a" {
		t.Fatalf("daemon cid.lease must Store: leased=%q ok=%v reply=%#v", leased, ok, got)
	}
}
