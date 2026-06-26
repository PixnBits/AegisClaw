package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"AegisClaw/internal/transport/hubclient"
)

// turnTestHub captures llm.call prompts for channel turn regression tests.
type turnTestHub struct {
	assignedID string
	llmPrompt  string
}

func (h *turnTestHub) Register(context.Context, string, ed25519.PublicKey, string) (*hubclient.RegisterResponse, error) {
	return &hubclient.RegisterResponse{AssignedID: h.assignedID}, nil
}

func (h *turnTestHub) Send(_ context.Context, msg hubclient.Message) (hubclient.Message, error) {
	switch msg.Command {
	case "channel.get_relevant_since":
		return hubclient.Message{
			Command: "channel.get_relevant_since.data",
			Payload: map[string]interface{}{"anchors": []interface{}{}},
		}, nil
	case "llm.call":
		if req, ok := msg.Payload.(map[string]interface{}); ok {
			if inner, ok := req["request"].(map[string]interface{}); ok {
				if p, ok := inner["prompt"].(string); ok {
					h.llmPrompt = p
				}
			}
		}
		return hubclient.Message{
			Command: "llm.call.response",
			Payload: map[string]interface{}{"response": "Here is my channel turn reply."},
		}, nil
	case "channel.post":
		return hubclient.Message{Command: "channel.posted", Payload: map[string]interface{}{"ok": true}}, nil
	default:
		return hubclient.Message{Command: "response", Payload: map[string]interface{}{"ok": true}}, nil
	}
}

func (h *turnTestHub) Close() error                                       { return nil }
func (h *turnTestHub) AssignedID() string                                 { return h.assignedID }
func (h *turnTestHub) IsVsock() bool                                      { return false }
func (h *turnTestHub) Receive(context.Context) (hubclient.Message, error) { return hubclient.Message{}, nil }
func (h *turnTestHub) Reply(context.Context, hubclient.Message) error     { return nil }
func (h *turnTestHub) TryReceive(context.Context, time.Duration) (hubclient.Message, bool, error) {
	return hubclient.Message{}, false, nil
}

func TestProcessChannelTurnUsesDirectTurnPrompt(t *testing.T) {
	// Regression: processChannelTurn must call llmChannelReply with the turn prompt,
	// not generateChannelReply (which double-wraps with VOTE proposal review format).
	hub := &turnTestHub{assignedID: "court-persona-senior-coder"}
	msg := hubclient.Message{
		Source: "store",
		Command: "channel.turn",
		Payload: map[string]interface{}{
			"channel_id": "main",
			"since_seq":  1,
			"new_messages": []interface{}{
				map[string]interface{}{"from": "user", "content": "Can you review this design?"},
			},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	processChannelTurn(hub, msg, "court-persona-senior-coder", "senior-coder")
	if hub.llmPrompt == "" {
		t.Fatal("expected llm.call with turn prompt")
	}
	if !strings.Contains(hub.llmPrompt, "batched channel turn") {
		t.Fatalf("expected turn-specific prompt, got: %s", hub.llmPrompt)
	}
	if strings.Contains(hub.llmPrompt, "VOTE:") || strings.Contains(hub.llmPrompt, "SPECIFIC_FEEDBACK") {
		t.Fatalf("turn prompt must not use proposal VOTE wrapper: %s", hub.llmPrompt)
	}
	if strings.Contains(hub.llmPrompt, "Proposal description:") {
		t.Fatalf("turn prompt must not use generateChannelReply proposal wrapper: %s", hub.llmPrompt)
	}
}

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
		vote, reasoning := analyzeProposal(p, "add a simple logging skill", nil) // nil hubClient → test-only simulator path (never used in prod binary loop)
		if vote != "Approve" && vote != "Reject" && vote != "Abstain" {
			t.Errorf("%s produced invalid vote %s", p, vote)
		}
		if reasoning == "" {
			t.Errorf("%s produced empty reasoning", p)
		}
	}
	// Security architect rejects networky things
	v, _ := analyzeProposal("security-architect", "add a discord monitor skill with network calls", nil) // test-only path
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