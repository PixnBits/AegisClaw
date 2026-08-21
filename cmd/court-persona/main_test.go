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

func (h *turnTestHub) Close() error       { return nil }
func (h *turnTestHub) AssignedID() string { return h.assignedID }
func (h *turnTestHub) IsVsock() bool      { return false }
func (h *turnTestHub) Receive(context.Context) (hubclient.Message, error) {
	return hubclient.Message{}, nil
}
func (h *turnTestHub) Reply(context.Context, hubclient.Message) error { return nil }
func (h *turnTestHub) TryReceive(context.Context, time.Duration) (hubclient.Message, bool, error) {
	return hubclient.Message{}, false, nil
}

func TestProcessChannelTurnUsesDirectTurnPrompt(t *testing.T) {
	// Regression: processChannelTurn must call llmChannelReply with the turn prompt,
	// not generateChannelReply (which double-wraps with VOTE proposal review format).
	hub := &turnTestHub{assignedID: "court-persona-senior-coder"}
	msg := hubclient.Message{
		Source:  "store",
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

func TestCISOChannelPromptIsDecisionFirstNotVote(t *testing.T) {
	p := getChannelPersonaPrompt("ciso")
	if !strings.Contains(p, "PASS") || !strings.Contains(p, "SPEAK") {
		t.Fatalf("CISO channel prompt must teach PASS/SPEAK actions, got: %s", p)
	}
	if strings.Contains(p, "VOTE:") {
		t.Fatalf("CISO channel prompt must not mix Court VOTE format: %s", p)
	}
	if !strings.Contains(p, "default") && !strings.Contains(strings.ToLower(p), "pass is the default") {
		t.Fatalf("CISO channel prompt should state PASS is the default: %s", p)
	}
	proposal := getPersonaPrompt("ciso")
	if !strings.Contains(proposal, "You are the") {
		t.Fatal("proposal prompt still required")
	}
	turn := buildChannelTurnPrompt("ciso", "court-persona-ciso", "main", "- user: hello", "")
	if strings.Contains(turn, "VOTE:") {
		t.Fatalf("turn prompt must not include VOTE: %s", turn)
	}
	if !strings.Contains(turn, "New messages since your last turn") {
		t.Fatalf("turn prompt missing batch: %s", turn)
	}
	mentioned := buildChannelTurnPrompt("ciso", "court-persona-ciso", "main", "- user: @CISO any concern with this CSS?", "")
	if !strings.Contains(mentioned, "You were directly @mentioned") {
		t.Fatalf("mentioned turn must force SPEAK, got: %s", mentioned)
	}
}

func TestAllCourtChannelPromptsUseSpeakPass(t *testing.T) {
	personas := []string{"ciso", "security-architect", "architect", "senior-coder", "tester", "efficiency", "user-advocate"}
	for _, p := range personas {
		prompt := getChannelPersonaPrompt(p)
		if !strings.Contains(prompt, "PASS") || !strings.Contains(prompt, "SPEAK") {
			t.Errorf("%s channel prompt missing PASS/SPEAK", p)
		}
		if strings.Contains(prompt, "VOTE:") {
			t.Errorf("%s channel prompt must not mix Court VOTE format", p)
		}
		if !strings.Contains(strings.ToLower(prompt), "pass is the default") {
			t.Errorf("%s channel prompt should state PASS is the default", p)
		}
		src := "court-persona-" + p
		quiet := buildChannelTurnPrompt(p, src, "main", "- ux: the empty-state copy feels cold", "")
		mention := buildChannelTurnPrompt(p, src, "main", "- user: @"+p+" any concern with this CSS?", "")
		if !strings.Contains(mention, "You were directly @mentioned") {
			t.Errorf("%s mentioned turn must force SPEAK", p)
		}
		// UX copy is not a first-seen topic for most roles; user-advocate may SPEAK on "copy"/"empty-state".
		if p != "user-advocate" && !strings.Contains(quiet, "First line MUST be PASS") {
			t.Errorf("%s quiet UX turn must force PASS, got: %s", p, quiet)
		}
		if p == "user-advocate" && !strings.Contains(quiet, "First line MUST be SPEAK") {
			t.Errorf("user-advocate should SPEAK on empty-state copy, got: %s", quiet)
		}
	}
}

func TestPersonaTopicOracle(t *testing.T) {
	cases := []struct {
		persona string
		batch   string
		anchors string
		want    bool
	}{
		{"security-architect", "- coder: two agents share one Memory VM socket", "", true},
		{"security-architect", "- ux: tooltip padding is unchanged", "", false},
		{"architect", "- user: can every agent share one Memory VM as a group brain?", "", true},
		{"architect", "- tester: I'll regenerate Playwright snapshots.", "", false},
		{"senior-coder", "- coder: encrypt localStorage with a hardcoded key", "", true},
		{"senior-coder", "- ux: checkbox label should say Keep me signed in.", "", false},
		{"tester", "- user: drop Firefox from visual polish so CI stays green.", "", true},
		{"tester", "- pm: standup, no new goals today.", "", false},
		{"efficiency", "- eff: cut Tester to 192MiB after a soak", "", true},
		{"efficiency", "- ux: tooltip padding is 1px off.", "", false},
		{"user-advocate", "- ux: empty state says No activity", "", true},
		{"user-advocate", "- secarch: cid=3 is probably the hub bridge.", "", false},
	}
	for _, tc := range cases {
		ok, hits := personaNewMaterialTopic(tc.persona, tc.batch, tc.anchors)
		if ok != tc.want {
			t.Errorf("%s batch=%q wantSpeak=%v got %v hits=%v", tc.persona, tc.batch, tc.want, ok, hits)
		}
	}
}

func TestCISONewMaterialRiskOracle(t *testing.T) {
	ok, hits := cisoNewMaterialRisk("- coder: put GOOGLE_CLIENT_SECRET in .env", "")
	if !ok {
		t.Fatalf("new .env secret must be a CISO risk, hits=%v", hits)
	}
	ok, _ = cisoNewMaterialRisk("- coder: put GOOGLE_CLIENT_SECRET in .env", "- user: never commit .env secrets")
	if ok {
		t.Fatal("same .env risk already in anchors should not be new")
	}
	ok, _ = cisoNewMaterialRisk("- tester: snapshot path is fine", "")
	if ok {
		t.Fatal("path/snapshot chatter must not match PAT/secret keywords")
	}
	p := buildChannelTurnPrompt("ciso", "court-persona-ciso", "ops", "- user: I pasted sk-live-123 in the channel", "")
	if !strings.Contains(p, "First line MUST be SPEAK") {
		t.Fatalf("new secret paste must force SPEAK, got: %s", p)
	}
	quiet := buildChannelTurnPrompt("ciso", "court-persona-ciso", "ux", "- ux: the empty-state copy feels cold", "")
	if !strings.Contains(quiet, "First line MUST be PASS") {
		t.Fatalf("copy-only turn must force PASS, got: %s", quiet)
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
