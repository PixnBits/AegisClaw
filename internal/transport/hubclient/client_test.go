// client_test.go — tests for the Phase 1 foundational hub transport client.
// These tests exercise the exact code paths that the real Agent Runtime (6-step loop)
// and Memory VM will use when communicating with AegisHub over both unix and (future) vsock.
//
// SPEC REFERENCES (must appear in every commit touching this package):
//   - docs/specs/aegishub.md §Handshake Sequence + §Authentication + error codes
//   - docs/specs/agent-runtime.md §Communication (vsock/JSON-RPC only)
//   - docs/prd/security-model.md §Communication & Mediation (fail-closed on any auth/ACL failure)
//   - docs/no-stubs-plan/phase-1.md 1.1a (transport client before any agent or memory changes)
//
// Testing strategy:
//   - Use net.Pipe() to create a fully in-memory bidirectional conn.
//   - Simulate the hub side of the register + one normal message exchange in the test goroutine.
//   - This gives hermetic, fast, root-free coverage of signing, context, error mapping, Close, etc.
//   - Vsock path is exercised only for "dial fails gracefully" (no real vsock listener in test env).
//
// Paranoid notes: we deliberately test the error paths (bad sig, missing register, etc.) because
// those are the fail-closed gates that protect the whole runtime.

package hubclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDialUnix_BadKey(t *testing.T) {
	// Pass a key with wrong length (the only material check we perform for speed;
	// a full ed25519.PrivateKeySize slice with garbage bytes would still "look" valid to us
	// and only fail later at the hub signature verification step — which is the correct
	// paranoid behavior).
	short := ed25519.PrivateKey(make([]byte, 10))
	_, err := DialUnix("/tmp/does-not-matter.sock", short)
	if err == nil || !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("expected ErrInvalidPublicKey for wrong-length key material, got: %v", err)
	}
}

func TestRegisterAndSend_HappyPath_UnixPipe(t *testing.T) {
	// Full end-to-end of the client using an in-memory pipe that simulates the hub.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// One side is the "client", the other side we drive manually to act like AegisHub.
	clientConn, hubConn := net.Pipe()

	// Build the client directly via the internal seam so we can inject the pipe.
	// (DialUnix would do a real dial; we want hermetic.)
	c := &client{
		conn:    clientConn,
		enc:     json.NewEncoder(clientConn),
		dec:     json.NewDecoder(clientConn),
		priv:    make([]byte, len(priv)),
		isVsock: false,
	}
	copy(c.priv, priv)

	// Hub simulation goroutine: handle exactly one register + one normal Send.
	go func() {
		defer hubConn.Close()

		hubDec := json.NewDecoder(hubConn)
		hubEnc := json.NewEncoder(hubConn)

		// 1. Expect register
		var reg Message
		if err := hubDec.Decode(&reg); err != nil {
			t.Logf("hub sim decode reg: %v", err)
			return
		}
		if reg.Command != "register" || reg.Destination != "hub" {
			t.Logf("hub sim: unexpected first message: %+v", reg)
			return
		}

		// Respond with successful registration (matching real hub shape)
		regResp := map[string]interface{}{
			"status":      "registered",
			"assigned_id": "agent-test-001",
			"acls":        []interface{}{},
			"version":     "phase1-test",
		}
		_ = hubEnc.Encode(regResp)

		// 2. Expect one normal message (the Send after register)
		var normalMsg Message
		if err := hubDec.Decode(&normalMsg); err != nil {
			t.Logf("hub sim decode normal: %v", err)
			return
		}
		if normalMsg.Command != "memory.get_context" {
			t.Logf("hub sim: unexpected command %s", normalMsg.Command)
			return
		}
		// Reply as the real memory (or any) component would via the hub
		reply := Message{
			Source:      "memory",
			Destination: normalMsg.Source,
			Command:     "memory.context",
			Payload: map[string]string{
				"short_term": "test context from simulated hub",
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Signature: "hub-reply-sig-not-checked-in-this-test",
		}
		_ = hubEnc.Encode(reply)
	}()

	// Now exercise the public API from the client side.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := c.Register(ctx, "agent-test-001", pub, "phase1-test")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if resp.AssignedID != "agent-test-001" {
		t.Errorf("unexpected assigned ID: %s", resp.AssignedID)
	}
	if c.AssignedID() != "agent-test-001" {
		t.Error("client did not store assigned ID")
	}

	// Now do a normal Send (this exercises signing + roundtrip)
	sendMsg := Message{
		Source:      c.AssignedID(),
		Destination: "memory",
		Command:     "memory.get_context",
		Payload:     map[string]string{"reason": "test-turn"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	reply, err := c.Send(ctx, sendMsg)
	if err != nil {
		t.Fatalf("Send after register failed: %v", err)
	}
	if reply.Command != "memory.context" {
		t.Errorf("unexpected reply command: %s", reply.Command)
	}

	_ = c.Close()
}

func TestSend_BeforeRegister_FailsClosed(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	c, err := DialUnix("/tmp/nonexistent-for-test.sock", priv) // dial will fail but we don't care
	if err == nil {
		// If the socket happens to exist in the env, close it and proceed.
		c.Close()
		c, _ = DialUnix("/tmp/nonexistent-for-test.sock", priv)
	}
	// Force a client that has no assigned ID (simulate post-dial pre-register state)
	// We do it by constructing directly.
	clientConn, _ := net.Pipe()
	badC := &client{
		conn: clientConn,
		enc:  json.NewEncoder(clientConn),
		dec:  json.NewDecoder(clientConn),
		priv: make([]byte, len(priv)),
	}
	copy(badC.priv, priv)

	ctx := context.Background()
	_, err = badC.Send(ctx, Message{Command: "anything"})
	if err == nil || !strings.Contains(err.Error(), "Register must succeed") {
		t.Fatalf("expected pre-register guard error, got: %v", err)
	}
	_ = badC.Close()
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		in  string
		out error
	}{
		{"ERR_ACL_VIOLATION", ErrACLViolation},
		{"ERR_INVALID_SIGNATURE", ErrInvalidSignature},
		{"ERR_SIGNATURE_REQUIRED", ErrSignatureRequired},
		{"SOME_RANDOM", ErrUnknown},
	}
	for _, tc := range cases {
		if got := mapHubError(tc.in); !errors.Is(got, tc.out) {
			t.Errorf("mapHubError(%q) = %v, want %v", tc.in, got, tc.out)
		}
	}
}

func TestVsockDial_GracefulFailure_NoRealListener(t *testing.T) {
	// We cannot easily create a real vsock listener without privileges and a running hub.
	// This test only verifies that DialVsock fails fast and returns a useful error
	// (the vsock path will be exercised for real in integration/chaos tests and on Linux
	// with a live daemon + Firecracker guest).
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, err := DialVsock(HostCID, HubVsockPort, priv)
	if err == nil {
		t.Fatal("expected dial error when no vsock listener present (as in this test env)")
	}
	// The error will be something like "no such file or device" or "connection refused"
	// from the vsock subsystem — we just assert it is not a panic or nil.
	if !strings.Contains(err.Error(), "vsock") && !strings.Contains(err.Error(), "connect") {
		t.Logf("vsock dial error (acceptable): %v", err)
	}
	ZeroPrivateKey(priv)
}

func TestZeroPrivateKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	original := make([]byte, len(priv))
	copy(original, priv)

	ZeroPrivateKey(priv)
	for i, b := range priv {
		if b != 0 {
			t.Errorf("byte %d not zeroed after ZeroPrivateKey (paranoid hygiene failure)", i)
		}
	}
	// Also verify we didn't clobber the copy we made
	if len(original) != ed25519.PrivateKeySize {
		t.Error("original copy length changed (impossible)")
	}
}
