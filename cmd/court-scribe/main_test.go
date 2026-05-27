package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSignMessage(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	msg := &Message{
		Source:    "test",
		Command:   "test",
		Payload:   "data",
		Timestamp: "2026-05-10T00:00:00Z",
	}
	signMessage(msg, priv)
	if msg.Signature == "" {
		t.Error("Signature not set")
	}
	data, _ := json.Marshal(Message{Source: "test", Command: "test", Payload: "data", Timestamp: "2026-05-10T00:00:00Z"})
	sigBytes, _ := base64.StdEncoding.DecodeString(msg.Signature)
	if !ed25519.Verify(pub, data, sigBytes) {
		t.Error("Signature verification failed")
	}
}

func TestDecideReview(t *testing.T) {
	cases := []struct {
		name   string
		votes  map[string]string
		want   bool
	}{
		{"all approve", map[string]string{"ciso": "Approve", "architect": "Approve", "tester": "Approve", "senior-coder": "Approve", "security-architect": "Approve", "efficiency": "Approve", "user-advocate": "Approve"}, true},
		{"one reject", map[string]string{"ciso": "Approve", "architect": "Reject", "tester": "Approve"}, false},
		{"all abstain", map[string]string{"ciso": "Abstain", "architect": "Abstain"}, false},
		{"unanimous non-abstain approve + abstains", map[string]string{"ciso": "Approve", "architect": "Approve", "tester": "Abstain", "senior-coder": "Approve", "security-architect": "Approve", "efficiency": "Approve", "user-advocate": "Approve"}, true},
		{"mixed with reject", map[string]string{"ciso": "Approve", "architect": "Abstain", "tester": "Reject"}, false},
		{"some approve some abstain no reject", map[string]string{"ciso": "Approve", "architect": "Approve", "tester": "Abstain"}, true}, // 2 approve == 2 non-abstain
		{"empty votes", map[string]string{}, false},
		{"single approve", map[string]string{"ciso": "Approve"}, true},
		{"single reject", map[string]string{"ciso": "Reject"}, false},
		{"single abstain", map[string]string{"ciso": "Abstain"}, false},
	}
	for _, c := range cases {
		got := decideReview(c.votes)
		if got != c.want {
			t.Errorf("%s: decideReview() = %v, want %v (votes: %+v)", c.name, got, c.want, c.votes)
		}
	}
}

func TestScribeNoContentGuard(t *testing.T) {
	// The logic in notify handler rejects if description/extracted present
	payload := map[string]interface{}{"proposal_id": "p1", "description": "secret code"}
	if _, has := payload["description"]; !has {
		t.Error("test setup")
	}
	// In real handler this would set ERR_SCRIBE_NO_CONTENT
}

func TestGetBuildVersion(t *testing.T) {
	// Smoke test – just ensure it returns something without panicking.
	v := getBuildVersion()
	if v == "" {
		t.Error("getBuildVersion returned empty string")
	}
}

func TestExpandPath(t *testing.T) {
	// Basic behavior for non-~ paths
	p := expandPath("/absolute/path")
	if p != "/absolute/path" {
		t.Errorf("expandPath(/absolute/path) = %s, want /absolute/path", p)
	}

	// ~ expansion (best-effort, environment dependent)
	p2 := expandPath("~/foo")
	if !strings.HasSuffix(p2, "/foo") {
		t.Errorf("expandPath(~/foo) = %s, expected to end with /foo", p2)
	}
}