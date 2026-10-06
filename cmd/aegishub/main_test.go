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
		// Command patterns keep raw prefix matching (not the ID boundary rule).
		{"coder*", "coderX", true},
		{"channel.*", "channel.post", true},
	}
	for _, tt := range tests {
		got := aclMatch(tt.pattern, tt.value)
		if got != tt.want {
			t.Errorf("aclMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

func TestACLIDMatch(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "anything", true},
		{"project-manager*", "project-manager", true},
		{"project-manager*", "project-manager-1", true},
		{"project-manager*", "project-managerX", false},
		{"coder*", "coder", true},
		{"coder*", "coder-1", true},
		{"coder*", "coderX", false},
		{"agent*", "agent", true},
		{"agent*", "agent-1", true},
		{"agent*", "agent1", false},
		{"court-persona-*", "court-persona-x", true},
		{"court-persona-*", "court-persona-ciso", true},
		{"court-persona-*", "court-persona", false},
		{"court-persona-ciso*", "court-persona-ciso", true},
		{"court-persona-ciso*", "court-persona-ciso-1", true},
		{"court-persona-ciso*", "court-persona-cisoX", false},
		{"hub-perm-fetch-*", "hub-perm-fetch-1", true},
		{"hub-perm-fetch-*", "hub-perm-fetchX", false},
		{"hub-perm-fetch-*", "hub-perm-fetch", false},
		{"daemon-internal*", "daemon-internal", true},
		{"daemon-internal*", "daemon-internal-1", true},
		{"daemon-internal*", "daemon-internal-fanout-3", true},
		{"daemon-internal*", "daemon-internalX", false},
		{"aegis-cli-internal*", "aegis-cli-internal", true},
		{"aegis-cli-internal*", "aegis-cli-internal-9", true},
		{"aegis-cli-internal*", "aegis-cli-internalX", false},
		{"memory*", "memory", true},
		{"memory*", "memory-session", true},
		{"memory*", "memoryX", false},
		{"memory.*", "memory.get_context", true},
		{"memory.*", "memoryfoo", false},
	}
	for _, tt := range tests {
		got := aclIDMatch(tt.pattern, tt.value)
		if got != tt.want {
			t.Errorf("aclIDMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
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
		{"coderX", "store", "channel.post", false},
		{"coderX", "network-boundary", "llm.call", false},
	}
	for _, c := range cases {
		if got := checkACL(c.src, c.dst, c.cmd); got != c.want {
			t.Errorf("checkACL(%q,%q,%q)=%v want %v", c.src, c.dst, c.cmd, got, c.want)
		}
	}
}

func TestRepoACLPermissionFetchAndStoreChannelReplies(t *testing.T) {
	origRules := aclRules
	origPath := aclFilePath
	origMod := lastACLModTime
	defer func() {
		aclRules = origRules
		aclFilePath = origPath
		lastACLModTime = origMod
	}()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Join(wd, "..", "..")
	t.Setenv("AEGIS_ACL_FILE", filepath.Join(repoRoot, "config", "acls.yaml"))
	loadACL()
	if len(aclRules) == 0 {
		t.Fatal("aclRules empty after loadACL")
	}

	cases := []struct {
		src, dst, cmd string
		want          bool
	}{
		{"store", "hub-perm-fetch-1", "permission.snapshot", true},
		{"store", "hub-perm-fetch-1", "error", true},
		{"store", "hub-perm-fetch-1", "response", true},
		{"hub-perm-fetch-1", "store", "permission.request", false},
		{"hub-perm-fetch-1", "store", "permission.snapshot", false},
		{"hub-perm-fetch", "store", "permission.snapshot", false},
		{"store", "hub-perm-fetchX", "permission.snapshot", false},
		{"store", "hub-perm-fetchX", "anything", false},
		{"store", "hub-perm-fetchX", "channel.post", false},
		{"store", "hub-perm-fetchX", "permission.grant", false},
		{"store", "hub-perm-fetch-1", "permission.grant", false},
		{"hub-perm-fetch-1", "store", "anything", false},
		{"hub-perm-fetch-1", "store", "channel.post", false},
		{"hub-perm-fetch-1", "store", "permission.grant", false},
		// Older store -> project-manager* channel.* rule, not the channel-context block.
		{"store", "project-manager-x", "channel.get_relevant_since.data", true},
		{"store", "project-manager", "channel.get_relevant_since.data", true},
		{"store", "project-managerX", "channel.get_relevant_since.data", false},
		{"store", "coderX", "channel.get_relevant_since.data", false},
		{"store", "coder-1", "channel.get_relevant_since.data", true},
		// No store → unknown dest rule for channel replies (catch-all is response/error/ping/pong/version only).
		{"store", "some-unknown-dest", "channel.get_relevant_since.data", false},
		// Role prefix is a dash boundary: project-managerX is not a project manager.
		{"project-managerX", "store", "channel.post", false},
		{"project-manager-1", "store", "channel.post", true},
		{"project-manager", "store", "channel.post", true},
		{"coderX", "channel-facilitator", "channel.turn_result", false},
		{"coder-1", "channel-facilitator", "channel.turn_result", true},
		{"daemon-internal", "store", "channel.list", true},
		{"daemon-internal-1", "store", "channel.list", true},
		{"daemon-internal-fanout-3", "store", "channel.list", true},
		{"aegis-cli-internal", "store", "proposal.list", true},
		{"aegis-cli-internal-99", "store", "proposal.list", true},
		{"aegis-cli-internalX", "store", "proposal.list", false},
	}
	roles := []string{"agent-x", "coder-x", "tester-x", "ciso-x", "architect-x", "researcher-x"}
	for _, role := range roles {
		for _, cmd := range []string{"channel.get_relevant_since.data", "channel.get_messages.data"} {
			cases = append(cases, struct {
				src, dst, cmd string
				want          bool
			}{"store", role, cmd, true})
		}
		for _, cmd := range []string{"channel.member_added", "channel.posted"} {
			cases = append(cases, struct {
				src, dst, cmd string
				want          bool
			}{"store", role, cmd, false})
		}
	}
	for _, c := range cases {
		if got := checkACL(c.src, c.dst, c.cmd); got != c.want {
			t.Errorf("checkACL(%q,%q,%q)=%v want %v", c.src, c.dst, c.cmd, got, c.want)
		}
	}
}

// TestRepoACLStoreToRoleSnapshotAndLLMDenied pins the #106 store → role
// reply rule to the two channel *.data commands. permission.snapshot and
// llm.call must stay denied so that rule cannot be widened silently.
// The *.data allows are the positive control: a deleted rule must fail
// the test, not look the same as a deny-all file.
func TestRepoACLStoreToRoleSnapshotAndLLMDenied(t *testing.T) {
	origRules := aclRules
	origPath := aclFilePath
	origMod := lastACLModTime
	defer func() {
		aclRules = origRules
		aclFilePath = origPath
		lastACLModTime = origMod
	}()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Join(wd, "..", "..")
	t.Setenv("AEGIS_ACL_FILE", filepath.Join(repoRoot, "config", "acls.yaml"))
	loadACL()
	if len(aclRules) == 0 {
		t.Fatal("aclRules empty after loadACL")
	}

	roles := []string{"coder-1", "agent-1", "tester-1", "ciso-1", "architect-1", "researcher-1"}
	for _, role := range roles {
		for _, cmd := range []string{"permission.snapshot", "llm.call"} {
			if checkACL("store", role, cmd) {
				t.Errorf("checkACL(store, %s, %s) = true, want deny", role, cmd)
			}
		}
		for _, cmd := range []string{"channel.get_relevant_since.data", "channel.get_messages.data"} {
			if !checkACL("store", role, cmd) {
				t.Errorf("checkACL(store, %s, %s) = false, want allow", role, cmd)
			}
		}
	}
}

// TestRepoACLLLMUsageNarrowed loads config/acls.yaml and pins usage to exact
// commands. network-boundary -> store allows llm.usage.record only.
// store -> network-boundary does not allow llm.usage.recorded: the record is a
// one-way hub push and Store does not reply. Neither rule may contain an
// llm wildcard (a pattern starting with "llm." and ending with "*").
// Portal reads (web-portal and daemon-internal* -> store) allow llm.usage.summary
// and llm.usage.recent only. llm.usage.record is denied at the ACL for those
// sources. Replies ride the existing store -> portal "*" rules.
// Any parsed rule whose source or destination is store fails if a command
// pattern starts with "llm" and contains "*" (llm.*, llm.usage.*).
// Guest llm.* rules (role <-> network-boundary) are pre-existing and out of scope.
//
// error stays allowed by the source "*" destination "*" catch-all for unrelated
// RPCs. Usage rejection is logged, not replied, so the network-boundary rule
// does not grant error or llm.usage.recorded.
func TestRepoACLLLMUsageNarrowed(t *testing.T) {
	origRules := aclRules
	origPath := aclFilePath
	origMod := lastACLModTime
	defer func() {
		aclRules = origRules
		aclFilePath = origPath
		lastACLModTime = origMod
	}()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv("AEGIS_ACL_FILE", filepath.Join(wd, "..", "..", "config", "acls.yaml"))
	loadACL()
	if len(aclRules) == 0 {
		t.Fatal("aclRules empty after loadACL")
	}

	if !checkACL("network-boundary", "store", "llm.usage.record") {
		t.Error("network-boundary -> store llm.usage.record = false, want allow")
	}
	// One-way push: Store does not send llm.usage.recorded, and it is not granted.
	if checkACL("store", "network-boundary", "llm.usage.recorded") {
		t.Error("store -> network-boundary llm.usage.recorded = true, want deny")
	}
	// error stays on the * -> * catch-all for unrelated RPCs, not for usage.
	if !checkACL("store", "network-boundary", "error") {
		t.Error("store -> network-boundary error = false, want allow")
	}
	for _, cmd := range []string{
		"llm.usage.summary", "llm.usage.recent", "llm.usage.recorded",
		"llm.usage.x", "llm.usage", "llm.usagex", "llm.chat", "llm.foo",
	} {
		if checkACL("network-boundary", "store", cmd) {
			t.Errorf("network-boundary -> store %s = true, want deny", cmd)
		}
	}
	for _, cmd := range []string{"llm.usage.record", "llm.usage.recorded", "llm.usage.summary", "llm.chat"} {
		if checkACL("store", "network-boundary", cmd) {
			t.Errorf("store -> network-boundary %s = true, want deny", cmd)
		}
	}
	portalSources := []string{"web-portal", "daemon-internal", "daemon-internal-1", "daemon-internal-42"}
	for _, src := range portalSources {
		for _, cmd := range []string{"llm.usage.summary", "llm.usage.recent"} {
			if !checkACL(src, "store", cmd) {
				t.Errorf("%s -> store %s = false, want allow", src, cmd)
			}
		}
		for _, cmd := range []string{
			"llm.usage.record", "llm.usage.recorded", "llm.usage.x",
			"llm.usage", "llm.usagex", "llm.chat", "llm.foo",
		} {
			if checkACL(src, "store", cmd) {
				t.Errorf("%s -> store %s = true, want deny", src, cmd)
			}
		}
		for _, cmd := range []string{"llm.usage.summary", "llm.usage.recent", "error"} {
			if !checkACL("store", src, cmd) {
				t.Errorf("store -> %s %s = false, want allow", src, cmd)
			}
		}
	}
	for _, src := range []string{"aegis-cli-internal", "coder-1", "store"} {
		if checkACL(src, "store", "llm.usage.summary") {
			t.Errorf("%s -> store llm.usage.summary = true, want deny", src)
		}
	}

	for _, rule := range aclRules {
		if rule.Source != "store" && rule.Destination != "store" {
			continue
		}
		for _, cmd := range rule.Commands {
			if strings.HasPrefix(cmd, "llm") && strings.Contains(cmd, "*") {
				t.Errorf("parsed rule %q -> %q contains llm wildcard command %q", rule.Source, rule.Destination, cmd)
			}
		}
	}
	nbStore := commandsBetween("network-boundary", "store")
	storeNB := commandsBetween("store", "network-boundary")
	if !containsCmd(nbStore, "llm.usage.record") || hasLLMDotWildcard(nbStore) {
		t.Errorf("network-boundary -> store commands = %v, want llm.usage.record and no llm.* wildcard", nbStore)
	}
	if containsCmd(storeNB, "llm.usage.recorded") || hasLLMDotWildcard(storeNB) {
		t.Errorf("store -> network-boundary commands = %v, want no llm.usage.recorded and no llm.* wildcard", storeNB)
	}
	for _, pair := range [][2]string{
		{"web-portal", "store"},
		{"daemon-internal", "store"},
		{"daemon-internal-*", "store"},
	} {
		cmds := commandsBetween(pair[0], pair[1])
		if !containsCmd(cmds, "llm.usage.summary") || !containsCmd(cmds, "llm.usage.recent") ||
			containsCmd(cmds, "llm.usage.*") || containsCmd(cmds, "llm.usage.record") || containsCmd(cmds, "llm.*") {
			t.Errorf("%s -> %s commands = %v", pair[0], pair[1], cmds)
		}
	}
}

func commandsBetween(src, dst string) []string {
	var cmds []string
	for _, rule := range aclRules {
		if rule.Source == src && rule.Destination == dst {
			cmds = append(cmds, rule.Commands...)
		}
	}
	return cmds
}

func containsCmd(cmds []string, want string) bool {
	for _, c := range cmds {
		if c == want {
			return true
		}
	}
	return false
}

// hasLLMDotWildcard reports a command pattern starting with "llm." and ending with "*".
func hasLLMDotWildcard(cmds []string) bool {
	for _, c := range cmds {
		if strings.HasPrefix(c, "llm.") && strings.HasSuffix(c, "*") {
			return true
		}
	}
	return false
}

func TestIsReservedHubID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"hub", true},
		{"hub-perm-fetch", true},
		{"hub-perm-fetch-123", true},
		{"hub-perm-fetcher", false},
		{"store", false},
		{"coder-1", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isReservedHubID(c.id); got != c.want {
			t.Errorf("isReservedHubID(%q)=%v want %v", c.id, got, c.want)
		}
	}
}

func TestReservedHubIDRegistrationRejected(t *testing.T) {
	const id = "hub-perm-fetch-123"
	registeredMutex.Lock()
	prev, had := registered[id]
	registeredMutex.Unlock()
	t.Cleanup(func() {
		registeredMutex.Lock()
		if had {
			registered[id] = prev
		} else {
			delete(registered, id)
		}
		registeredMutex.Unlock()
	})

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(hub, &sync.Map{})
	}()
	reg := Message{
		Source:      id,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": base64.StdEncoding.EncodeToString(pub), "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("register decode: %v", err)
	}
	errVal, _ := resp["error"].(string)
	if !strings.Contains(errVal, "ERR_RESERVED_ID") {
		t.Fatalf("reply = %#v, want ERR_RESERVED_ID", resp)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleConnection did not return")
	}
	registeredMutex.RLock()
	_, present := registered[id]
	registeredMutex.RUnlock()
	if present {
		t.Fatalf("registered[%q] must be absent", id)
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

func TestDeliverPendingRPC_PermissionSnapshotReplyCompletesMatchingWaiter(t *testing.T) {
	requester := "hub-perm-fetch-1"
	waitCh := registerPendingRPC(requester, "store", "permission.snapshot")
	defer clearPendingRPC(requester)

	turn := Message{
		Source:      "channel-facilitator-out-1",
		Destination: requester,
		Command:     "channel.turn",
	}
	if deliverPendingRPC(turn) {
		t.Fatal("channel.turn must not complete a permission.snapshot waiter")
	}
	select {
	case got := <-waitCh:
		t.Fatalf("channel.turn delivered %s to permission.snapshot waiter", got.Command)
	default:
	}

	// Same destination as a waiter, but that waiter asked for llm.call.
	llmReq := "llm-waiter-not-perm"
	llmCh := registerPendingRPC(llmReq, "store", "llm.call")
	defer clearPendingRPC(llmReq)
	steal := Message{
		Source:      "store",
		Destination: llmReq,
		Command:     "permission.snapshot",
		Payload: map[string]interface{}{
			"version": int64(1),
			"subject": "coder-1",
		},
	}
	if deliverPendingRPC(steal) {
		t.Fatal("permission.snapshot must not complete an llm.call waiter")
	}
	select {
	case got := <-llmCh:
		t.Fatalf("llm.call waiter received %s", got.Command)
	default:
	}

	reply := Message{
		Source:      "store",
		Destination: requester,
		Command:     "permission.snapshot",
		Payload: map[string]interface{}{
			"version":       int64(3),
			"subject":       "coder-1",
			"allowed_tools": map[string]bool{"channel.post": true},
			"visible_tools": map[string]bool{"channel.post": true},
		},
	}
	if !deliverPendingRPC(reply) {
		t.Fatal("permission.snapshot from store should complete the hub-perm-fetch waiter")
	}
	select {
	case got := <-waitCh:
		if got.Command != "permission.snapshot" {
			t.Fatalf("waiter got %s", got.Command)
		}
		if got.Source != "store" {
			t.Fatalf("waiter source %s", got.Source)
		}
		payload, ok := got.Payload.(map[string]interface{})
		if !ok || payload["version"] != int64(3) {
			t.Fatalf("waiter payload %#v", got.Payload)
		}
	default:
		t.Fatal("waiter did not receive permission.snapshot")
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
	if !isOneWayHubPush("llm.usage.record") {
		t.Fatal("llm.usage.record must be a one-way push")
	}
	if isOneWayHubPush("llm.usage.recorded") {
		t.Fatal("llm.usage.recorded is not a push")
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
		t.Fatalf("miss without handshake must not ingest file: tenant=%q err=%v", got, err)
	}
	if !hublease.StoreLeaseIfAbsentOrSame(cid, pubAStr) {
		t.Fatal("verified handshake CAS fill")
	}
	got, err = tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("after handshake CAS fill: tenant=%q err=%v, want tenant-a", got, err)
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
	got, err = tenantForGit(pubAStr, addr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("after helper close, same CID+A must still be tenant-a (handshake fill): tenant=%q err=%v", got, err)
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
	got, err = tenantForGit(pubBStr, addr)
	if err == nil || got != "" {
		t.Fatalf("overwrite leftover with new pub must stay miss (no file ingest): tenant=%q err=%v", got, err)
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

func startVMVsockSession(t *testing.T, addr net.Addr, priv ed25519.PrivateKey, pub string) (net.Conn, <-chan struct{}) {
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
	if len(priv) > 0 {
		body, _ := json.Marshal(reg)
		reg.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
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
	vmClient, vmDone := startVMVsockSession(t, addr, privA, pubAStr)

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
	deny := []string{"git-remote-hub", "guest-vm", "agent-1", "aegis-cli-internal", "store", "daemon-internal", "daemon-temp-1", "aegis-daemon-temp", "aegis-daemon-temp-3"}
	for _, id := range deny {
		if daemonMayUnleaseCID(id, wire, dummy) {
			t.Fatalf("dummy sig must not allow cid.unlease from %q", id)
		}
	}
}

func sendCIDUnleaseRPC(t *testing.T, remote net.Addr, source, victimPub string) map[string]interface{} {
	t.Helper()
	return sendCIDCommandRPC(t, remote, source, "cid.unlease", victimPub)
}

func sendCIDLeaseRPC(t *testing.T, remote net.Addr, source, pub string) map[string]interface{} {
	t.Helper()
	return sendCIDCommandRPC(t, remote, source, "cid.lease", pub)
}

func sendCIDCommandRPC(t *testing.T, remote net.Addr, source, command, pub string) map[string]interface{} {
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
		Command:     command,
		Payload:     map[string]interface{}{"cid": uint32(42), "public_key": pub},
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
	// Guest vsock must not cid.unlease even after register (handshake does not Store).
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
	if !hublease.StoreLeaseIfAbsentOrSame(42, "pub-a") {
		t.Fatal("memory must hold the CID lease for daemon unlease")
	}
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
}

func TestGuestVsockRegisterDoesNotStoreOrUnpoison(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")

	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAStr := base64.StdEncoding.EncodeToString(pubA)
	addr := &vsock.Addr{ContextID: 42, Port: 9999}

	client, done := startVMVsockSession(t, addr, nil, pubAStr)
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("guest vsock register must not Store a CID lease")
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("VM session handleConnection did not return")
	}

	hublease.StoreLease(42, pubAStr)
	if !hublease.UnleaseCID(42, pubAStr) {
		t.Fatal("setup unlease")
	}
	client, done = startVMVsockSession(t, addr, nil, pubAStr)
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("unsigned guest vsock register must not fill the empty CID after unlease")
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("VM session handleConnection did not return")
	}
}

func TestHandshakeConfirmMismatchDoesNotOverwrite(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")

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
	hublease.StoreLease(42, pubAStr)
	addr := &vsock.Addr{ContextID: 42, Port: 9999}
	client, done := startVMVsockSession(t, addr, nil, pubBStr)
	leased, ok := hublease.LoadLease(42)
	if !ok || leased != pubAStr {
		t.Fatalf("handshake mismatch must not overwrite lease: leased=%q ok=%v", leased, ok)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("VM session handleConnection did not return")
	}
}

func TestCIDLeaseDaemonOnlyAndCAS(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")

	const cid uint32 = 42
	unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
	guestAddr := &vsock.Addr{ContextID: 99, Port: 9999}

	got := sendCIDLeaseRPC(t, unixAddr, "daemon", "pub-a")
	if leased, ok := hublease.LoadLease(cid); ok {
		t.Fatalf("daemon cid.lease must not fill git-connect; reply=%#v leased=%q ok=%v", got, leased, ok)
	}

	sendCIDLeaseRPC(t, guestAddr, "guest-vm", "pub-b")
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatal("guest must not cid.lease")
	}

	sendCIDLeaseRPC(t, unixAddr, "store", "pub-b")
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatal("store must not cid.lease")
	}

	sendCIDLeaseRPC(t, unixAddr, "daemon-temp-1", "pub-b")
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatal("daemon-temp-* must not cid.lease")
	}

	sendCIDLeaseRPC(t, unixAddr, "git-remote-hub", "pub-b")
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatal("git-remote-hub must not cid.lease")
	}

	got = sendCIDLeaseRPC(t, unixAddr, "daemon", "pub-b")
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatalf("daemon cid.lease must not fill; reply=%#v", got)
	}
}

func TestGuestCannotCIDLeaseEmpty(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")
	guestAddr := &vsock.Addr{ContextID: 42, Port: 9999}
	sendCIDLeaseRPC(t, guestAddr, "guest-vm", "pub-a")
	if leased, ok := hublease.LoadLease(42); ok {
		t.Fatalf("guest cid.lease must not fill empty lease: leased=%q", leased)
	}
}

func TestReloadOnMissIngestsLiveFileRow(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)

	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAStr := base64.StdEncoding.EncodeToString(pubA)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	cidPath := filepath.Join(dir, "cid-keys.json")
	identJSON, err := json.Marshal(map[string]string{pubAStr: "tenant-a"})
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
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("setup: memory must be empty before miss reload")
	}
	got, err := tenantForGit(pubAStr, addr)
	if err == nil || got != "" {
		t.Fatalf("miss must not ingest live file row: tenant=%q err=%v", got, err)
	}
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("miss must not ingest CID file")
	}
}

func TestHandshakeDoesNotIngestFileRow(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	t.Setenv("AEGIS_DEV_MODE", "1")

	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAStr := base64.StdEncoding.EncodeToString(pubA)
	dir := t.TempDir()
	cidPath := filepath.Join(dir, "cid-keys.json")
	cidJSON, err := json.Marshal(map[string]string{"42": pubAStr})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cidPath, cidJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_CID_KEYS", cidPath)

	addr := &vsock.Addr{ContextID: 42, Port: 9999}
	client, done := startVMVsockSession(t, addr, nil, pubAStr)
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("handshake confirm must not Store/ingest even when the file has a live row")
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("VM session handleConnection did not return")
	}
}

func signGuestRegister(priv ed25519.PrivateKey, pub string) Message {
	msg := Message{
		Source:      "guest-vm",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pub, "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(msg)
	msg.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	return msg
}

func guestVsockHandshake(t *testing.T, addr net.Addr, reg Message) map[string]interface{} {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(&remoteAddrConn{Conn: hub, remote: addr}, &sync.Map{})
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

func TestUnsignedVsockRegisterDoesNotStore(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubStr := base64.StdEncoding.EncodeToString(pub)
	const cid uint32 = 42
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	payload := map[string]string{"public_key": pubStr, "version": "1"}
	cases := []Message{
		{Source: "guest-vm", Destination: "hub", Command: "register", Payload: payload, Timestamp: time.Now().UTC().Format(time.RFC3339)},
		{Source: "guest-vm", Destination: "hub", Command: "register", Payload: payload, Timestamp: time.Now().UTC().Format(time.RFC3339), Signature: "dummy"},
	}
	for i, reg := range cases {
		resp := guestVsockHandshake(t, addr, reg)
		if _, ok := resp["error"]; ok {
			// register may still succeed; only Store is forbidden
		}
		if _, ok := hublease.LoadLease(cid); ok {
			t.Fatalf("case %d: unsigned vsock register must not Store", i)
		}
	}
}

func TestUnrosteredVsockRegisterDoesNotStore(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubStr := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	if err := os.WriteFile(identPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	const cid uint32 = 42
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	guestVsockHandshake(t, addr, signGuestRegister(priv, pubStr))
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatal("unrostered vsock register must not Store")
	}
}

func TestVerifiedRosteredHandshakeCASFills(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubStr := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	identJSON, err := json.Marshal(map[string]string{pubStr: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	const cid uint32 = 42
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	guestVsockHandshake(t, addr, signGuestRegister(priv, pubStr))
	got, ok := hublease.LoadLease(cid)
	if !ok || got != pubStr {
		t.Fatalf("verified rostered handshake CAS fill: got %q ok=%v", got, ok)
	}
}

func TestSecondGuestDifferentPubDoesNotOverwrite(t *testing.T) {
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
	identJSON, err := json.Marshal(map[string]string{pubAStr: "tenant-a", pubBStr: "tenant-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	const cid uint32 = 42
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	guestVsockHandshake(t, addr, signGuestRegister(privA, pubAStr))
	guestVsockHandshake(t, addr, signGuestRegister(privB, pubBStr))
	got, ok := hublease.LoadLease(cid)
	if !ok || got != pubAStr {
		t.Fatalf("second guest different pub must not overwrite: got %q ok=%v", got, ok)
	}
}

func TestHandshakeAfterStopVMDoesNotClearClosed(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubStr := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	identJSON, err := json.Marshal(map[string]string{pubStr: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	const cid uint32 = 42
	hublease.StoreLease(cid, pubStr)
	if !hublease.UnleaseCID(cid, pubStr) {
		t.Fatal("StopVM poison")
	}
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	guestVsockHandshake(t, addr, signGuestRegister(priv, pubStr))
	if _, ok := hublease.LoadLease(cid); ok {
		t.Fatal("same pub after UnleaseCID must not fill")
	}
	closed, ok := hublease.ClosedPub(cid)
	if !ok || closed != pubStr {
		t.Fatalf("after StopVM poison, handshake must not ClearClosed: closed=%q ok=%v", closed, ok)
	}
}

func TestHandshakeAfterStopVMDifferentPubClearsClosed(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	pubA, _, err := ed25519.GenerateKey(rand.Reader)
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
	identJSON, err := json.Marshal(map[string]string{pubAStr: "tenant-a", pubBStr: "tenant-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	const cid uint32 = 42
	hublease.StoreLease(cid, pubAStr)
	if !hublease.UnleaseCID(cid, pubAStr) {
		t.Fatal("StopVM poison")
	}
	addr := &vsock.Addr{ContextID: cid, Port: 9999}
	guestVsockHandshake(t, addr, signGuestRegister(privB, pubBStr))
	got, ok := hublease.LoadLease(cid)
	if !ok || got != pubBStr {
		t.Fatalf("different pub may fill after StopVM: got %q ok=%v", got, ok)
	}
	if closed, ok := hublease.ClosedPub(cid); ok {
		t.Fatalf("different pub fill must ClearClosed, still %q", closed)
	}
}

// TestWebPortalReregisterSurvivesOlderClose is the re-register race:
// replacing registered[id] must not let the older connection's close delete
// the new registration. The live connection must still be authorized.
func TestWebPortalReregisterSurvivesOlderClose(t *testing.T) {
	t.Setenv("AEGIS_DEV_MODE", "1")

	prevACL := aclRules
	t.Cleanup(func() {
		aclRules = prevACL
		registeredMutex.Lock()
		delete(registered, "web-portal")
		registeredMutex.Unlock()
	})
	aclRules = []ACLRule{{
		Source:      "web-portal",
		Destination: "hub",
		Commands:    []string{"component.list"},
	}}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conns := &sync.Map{}
	client1, _, done1 := registerTestComponent(t, conns, "web-portal", pub)
	client2, dec2, done2 := registerTestComponent(t, conns, "web-portal", pub)
	t.Cleanup(func() {
		_ = client1.Close()
		_ = client2.Close()
	})

	registeredMutex.RLock()
	second := registered["web-portal"]
	registeredMutex.RUnlock()
	if second == nil || second.Encoders == nil {
		t.Fatal("second registration missing before older close")
	}
	secondEnc := second.Encoders
	connBefore, ok := conns.Load("web-portal")
	if !ok || connBefore == nil {
		t.Fatal("conns missing second connection")
	}

	if err := client1.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done1:
	case <-time.After(3 * time.Second):
		t.Fatal("older connection did not finish")
	}

	registeredMutex.RLock()
	got := registered["web-portal"]
	registeredMutex.RUnlock()
	if got == nil || got.Encoders != secondEnc {
		t.Fatal("older close removed the newer registration")
	}
	connAfter, ok := conns.Load("web-portal")
	if !ok || connAfter != connBefore {
		t.Fatal("older close removed the newer conns entry")
	}

	msg := Message{
		Source:      "web-portal",
		Destination: "hub",
		Command:     "component.list",
		Payload:     map[string]string{},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Signature:   "dummy",
	}
	_ = client2.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client2).Encode(msg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := dec2.Decode(&resp); err != nil {
		t.Fatalf("component.list decode: %v", err)
	}
	if errVal, ok := resp["error"]; ok {
		t.Fatalf("live connection after older close: %v", errVal)
	}
	comps, ok := resp["components"].([]interface{})
	if !ok {
		t.Fatalf("expected component.list, got %#v", resp)
	}
	found := false
	for _, c := range comps {
		row, _ := c.(map[string]interface{})
		if row["id"] == "web-portal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web-portal missing from component.list: %#v", resp)
	}

	if err := client2.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done2:
	case <-time.After(3 * time.Second):
		t.Fatal("live connection did not finish")
	}
	registeredMutex.RLock()
	_, still := registered["web-portal"]
	registeredMutex.RUnlock()
	if still {
		t.Fatal("closing the owning connection left the registration in place")
	}
	if _, ok := conns.Load("web-portal"); ok {
		t.Fatal("closing the owning connection left the conns entry in place")
	}
}

func registerTestComponent(t *testing.T, conns *sync.Map, id string, pub ed25519.PublicKey) (net.Conn, *json.Decoder, <-chan struct{}) {
	t.Helper()
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(hub, conns)
	}()
	reg := Message{
		Source:      id,
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": base64.StdEncoding.EncodeToString(pub),
			"version":    "test",
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Signature: "dummy",
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(client)
	var resp map[string]interface{}
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("register %s decode: %v", id, err)
	}
	if e, ok := resp["error"]; ok {
		t.Fatalf("register %s error: %v", id, e)
	}
	if resp["status"] != "registered" || resp["assigned_id"] != id {
		t.Fatalf("register %s response: %#v", id, resp)
	}
	_ = client.SetDeadline(time.Time{})
	return client, dec, done
}

// TestRepoACLStoreSecurityStats pins store.security_stats (#154) to the
// daemon's internal clients: exact command, no wildcard. The portal, guests,
// network-boundary and Court personas are denied. The reply rides the
// existing store -> daemon-internal* "*" rule.
func TestRepoACLStoreSecurityStats(t *testing.T) {
	origRules := aclRules
	origPath := aclFilePath
	origMod := lastACLModTime
	defer func() {
		aclRules = origRules
		aclFilePath = origPath
		lastACLModTime = origMod
	}()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv("AEGIS_ACL_FILE", filepath.Join(wd, "..", "..", "config", "acls.yaml"))
	loadACL()
	if len(aclRules) == 0 {
		t.Fatal("aclRules empty after loadACL")
	}

	for _, src := range []string{"daemon-internal", "daemon-internal-1", "daemon-internal-42"} {
		if !checkACL(src, "store", "store.security_stats") {
			t.Errorf("%s -> store store.security_stats = false, want allow", src)
		}
		if !checkACL("store", src, "store.security_stats") {
			t.Errorf("store -> %s store.security_stats reply = false, want allow", src)
		}
		// Exact: no neighbouring store.* command rides along.
		for _, cmd := range []string{"store.security", "store.security_statsx", "store.wipe", "store.x"} {
			if checkACL(src, "store", cmd) {
				t.Errorf("%s -> store %s = true, want deny", src, cmd)
			}
		}
	}
	for _, src := range []string{
		"web-portal", "agent", "agent-1", "agent-coder-7", "network-boundary",
		"court-persona-ciso", "court-persona-tester", "builder-1", "coder-1", "project-manager-main",
	} {
		if checkACL(src, "store", "store.security_stats") {
			t.Errorf("%s -> store store.security_stats = true, want deny", src)
		}
	}
	// Both daemon rules carry the exact command, so removing the broader
	// "daemon-internal*" rule later can't silently drop it for daemon-internal-N.
	granted := map[string]bool{}
	for _, r := range aclRules {
		if r.Destination != "store" {
			continue
		}
		for _, c := range r.Commands {
			if c == "store.security_stats" {
				granted[r.Source] = true
			}
		}
	}
	for _, src := range []string{"daemon-internal*", "daemon-internal-*"} {
		if !granted[src] {
			t.Errorf("rule %s -> store does not list store.security_stats", src)
		}
	}
	if len(granted) != 2 {
		t.Errorf("store.security_stats granted to %v, want exactly daemon-internal* and daemon-internal-*", granted)
	}
	// No rule with destination store may grant a store.* wildcard.
	for _, r := range aclRules {
		if r.Destination != "store" {
			continue
		}
		for _, c := range r.Commands {
			if strings.HasPrefix(c, "store.") && strings.Contains(c, "*") {
				t.Errorf("rule %s -> store has wildcard %q", r.Source, c)
			}
		}
	}
}
