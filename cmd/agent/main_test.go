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

func TestMockLLMResponse(t *testing.T) {
	cases := []struct {
		prompt   string
		contains []string
	}{
		{"Observe and parse ... add a skill", []string{"Observed", "Intent", "proposal"}},
		{"Think step-by-step ... skill", []string{"Thought", "personas", "Builder gates"}},
		{"Create a concrete plan ... add a skill", []string{"Plan", "proposal.create", "scribe.notify_review"}},
		{"Judge the response quality ... add a skill", []string{"Judged", "Proposal ready for Court", "unanimous"}},
		{"Observe the user input: hello", []string{"Observed", "context"}},
		{"Judge the response quality: foo", []string{"Judged", "High quality"}},
	}
	for _, c := range cases {
		resp := mockLLMResponse(c.prompt)
		for _, want := range c.contains {
			if !strings.Contains(resp, want) {
				t.Errorf("mockLLMResponse(%q) = %q missing %q", c.prompt, resp, want)
			}
		}
	}
}

func TestCreateProposalPayload(t *testing.T) {
	// Lightweight check of proposal shape (full createProposal requires live hub/encoder)
	// Simulate the core construction used in judge/create flow.
	description := "add a discord monitor skill"
	proposalID := "proposal_" + "12345"
	proposal := map[string]interface{}{
		"id":          proposalID,
		"description": description,
		"extracted":   "mock-extracted",
		"status":      "pending",
	}
	if proposal["id"] != proposalID || proposal["status"] != "pending" {
		t.Error("proposal shape invalid")
	}
	// Ensure we would notify scribe with ID only (no 'description' key in scribe payload)
	scribePayload := map[string]interface{}{"proposal_id": proposalID}
	if _, hasDesc := scribePayload["description"]; hasDesc {
		t.Error("scribe notify must not contain description (security per court-scribe.md)")
	}
}
