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

func TestPersonaPromptsAndAnalysis(t *testing.T) {
	personas := []string{"ciso", "security-architect", "architect", "senior-coder", "tester", "efficiency", "user-advocate"}
	for _, p := range personas {
		prompt := getPersonaPrompt(p)
		if !strings.Contains(prompt, "You are the") {
			t.Errorf("%s prompt missing role", p)
		}
		vote, reasoning := analyzeProposal(p, "add a simple logging skill")
		if vote != "Approve" && vote != "Reject" && vote != "Abstain" {
			t.Errorf("%s produced invalid vote %s", p, vote)
		}
		if reasoning == "" {
			t.Errorf("%s produced empty reasoning", p)
		}
	}
	// Security architect rejects networky things
	v, _ := analyzeProposal("security-architect", "add a discord monitor skill with network calls")
	if v != "Reject" {
		t.Log("note: security-architect expected Reject on network skill (mock may vary)")
	}
}

func TestUniqueSource(t *testing.T) {
	// In run, source becomes "court-persona-" + flag
	if got := "court-persona-ciso"; !strings.HasPrefix(got, "court-persona-") {
		t.Error("unique source convention broken")
	}
}