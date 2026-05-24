package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const testHubSocketPath = "/tmp/aegishub_test.sock"

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
	// Clean up
	os.Remove(testHubSocketPath)

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
	cmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+testHubSocketPath, "AEGIS_DEV_MODE=1", "AEGIS_ACL_FILE="+aclPath)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for socket
	time.Sleep(100 * time.Millisecond)

	// Connect client1
	conn1, err := net.Dial("unix", testHubSocketPath)
	if err != nil {
		t.Fatalf("Failed to connect client1: %v", err)
	}
	defer conn1.Close()

	// Connect client2
	conn2, err := net.Dial("unix", testHubSocketPath)
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
	}
	for _, c := range cases {
		if got := checkACL(c.src, c.dst, c.cmd); got != c.want {
			t.Errorf("checkACL(%q,%q,%q)=%v want %v", c.src, c.dst, c.cmd, got, c.want)
		}
	}
}
