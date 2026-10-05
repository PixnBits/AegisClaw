package main

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"AegisClaw/internal/transport/hubclient"
)

func TestMentionedProseIfDropped(t *testing.T) {
	if got := mentionedProseIfDropped("PASS"); got != "" {
		t.Fatalf("PASS must stay silent, got %q", got)
	}
	if got := mentionedProseIfDropped("I'll bump the button padding 2px."); got == "" {
		t.Fatal("bare assignment ack must post when @mentioned")
	}
	if got := mentionedProseIfDropped("@ProjectManager which repo and path?"); got == "" {
		t.Fatal("asking for repo/path must post when @mentioned")
	}
}

func TestLooksLikeProgressClaim(t *testing.T) {
	if !looksLikeProgressClaim("I'll adjust the login button padding to fix the 2px offset issue.") {
		t.Fatal("claimed CSS tweak without a path must count as invented progress")
	}
	if looksLikeProgressClaim("I can take this as Coder — @ProjectManager, which repo and path? I have not changed any code yet.") {
		t.Fatal("asking for repo/path must not count as a progress claim")
	}
	if got := askForWorkTarget("CISO"); strings.Contains(strings.ToLower(got), "which repo") {
		t.Fatalf("advisory CISO fallback must not demand a repo, got %q", got)
	}
	if got := askForWorkTarget("Coder"); !strings.Contains(strings.ToLower(got), "which repo") {
		t.Fatalf("coder fallback must still ask for a path, got %q", got)
	}
	if batchNamesWorkTarget("from: user: The login button padding is 2px off. CSS only.", "") {
		t.Fatal("CSS goal with no path must not look like a work target")
	}
	if !batchNamesWorkTarget("from: project-manager: @Coder tweak web-portal/src/LoginButton.css padding.", "") {
		t.Fatal("named file path must count as a work target")
	}
}

func TestLooksLikeInternalDump(t *testing.T) {
	dump := `{
  "intent": "request_information",
  "entities": {"request_type": "repository_and_file_path"},
  "requires_proposal": false,
  "tool_calls": [],
  "observation": {"summary": "User is requesting information"}
}`
	if !looksLikeInternalDump(dump) {
		t.Fatal("skill-planner JSON must not be posted to the channel")
	}
	if got := mentionedProseIfDropped(dump); got != "" {
		t.Fatalf("dump must not recover as mention prose, got %q", got)
	}
	if looksLikeInternalDump("I can take this as Tester — @ProjectManager, which repo and path?") {
		t.Fatal("plain ask must not look like an internal dump")
	}
	md := "## Request Analysis\nThe user is asking for the repository.\n## Required Skills/Tools\n1. **discord_monitor**"
	if !looksLikeInternalDump(md) {
		t.Fatal("markdown skill-planner dump must not be posted")
	}
}

type agentTestHub struct {
	id      string
	sends   []hubclient.Message
	replies []hubclient.Message
}

func (h *agentTestHub) Register(context.Context, string, ed25519.PublicKey, string) (*hubclient.RegisterResponse, error) {
	return &hubclient.RegisterResponse{AssignedID: h.id}, nil
}
func (h *agentTestHub) Send(_ context.Context, msg hubclient.Message) (hubclient.Message, error) {
	h.sends = append(h.sends, msg)
	return hubclient.Message{Command: "response"}, nil
}
func (h *agentTestHub) Reply(_ context.Context, msg hubclient.Message) error {
	h.replies = append(h.replies, msg)
	return nil
}
func (h *agentTestHub) Close() error       { return nil }
func (h *agentTestHub) AssignedID() string { return h.id }
func (h *agentTestHub) IsVsock() bool      { return false }
func (h *agentTestHub) Receive(context.Context) (hubclient.Message, error) {
	return hubclient.Message{}, nil
}
func (h *agentTestHub) TryReceive(context.Context, time.Duration) (hubclient.Message, bool, error) {
	return hubclient.Message{}, false, nil
}

func TestPortalChatIsNotPMDirectTask(t *testing.T) {
	msg := hubclient.Message{
		Source:  "web-portal",
		Command: "chat.message",
		Payload: map[string]interface{}{"content": "hi", "from": "project-manager"},
	}
	if isPMDirectTask(msg) {
		t.Fatal("portal chat must stay on the normal turn loop even if payload names the PM")
	}
	if !isPMDirectTask(hubclient.Message{Source: "project-manager-main", Command: "chat.message"}) {
		t.Fatal("project-manager source must be a direct task")
	}
}

func TestPMDirectTaskRepliesToPMNotChannel(t *testing.T) {
	hub := &agentTestHub{id: "coder"}
	msg := hubclient.Message{
		Source:      "project-manager",
		Destination: "coder",
		Command:     "chat.message",
		Payload: map[string]interface{}{
			"from":     "project-manager",
			"reply_to": "project-manager",
			"goal":     "Fix the docs.",
			"plan":     "Coder updates the docs.",
			"content":  "Reply to the Project Manager.",
		},
	}
	llm := func(_ context.Context, p string) (string, error) {
		if !strings.Contains(p, "Fix the docs.") || !strings.Contains(p, "Do not post to a channel") {
			t.Fatalf("prompt missing task: %s", p)
		}
		return "I can update the docs once I have the path.", nil
	}
	if !handleAgentMessage(hub, msg, nil, llm) {
		t.Fatal("handler should keep running")
	}
	if len(hub.sends) != 0 {
		t.Fatalf("DM path must not Send (no channel.post), got %+v", hub.sends)
	}
	if len(hub.replies) != 1 {
		t.Fatalf("expected one hub reply, got %d", len(hub.replies))
	}
	got := hub.replies[0]
	if got.Destination != "project-manager" || got.Command != "response" {
		t.Fatalf("reply = %+v", got)
	}
	p, _ := got.Payload.(map[string]interface{})
	content, _ := p["content"].(string)
	if !strings.Contains(content, "path") {
		t.Fatalf("content = %q", content)
	}
}

func TestPMDirectTaskDropsInternalDump(t *testing.T) {
	hub := &agentTestHub{id: "tester"}
	msg := hubclient.Message{
		Source:  "project-manager",
		Command: "chat.message",
		Payload: map[string]interface{}{"goal": "Look at this.", "content": "Review."},
	}
	llm := func(context.Context, string) (string, error) {
		return `{"intent":"request_information","tool_calls":[]}`, nil
	}
	processPMDirectTask(hub, msg, llm)
	if len(hub.sends) != 0 {
		t.Fatal("dump path must not channel.post")
	}
	if len(hub.replies) != 1 {
		t.Fatalf("replies = %d", len(hub.replies))
	}
	p := hub.replies[0].Payload.(map[string]interface{})
	content, _ := p["content"].(string)
	if strings.Contains(content, "intent") || content == "" {
		t.Fatalf("dump must not be the reply, got %q", content)
	}
}

func TestAgentSkillIndex_ListSkills(t *testing.T) {
	idx := NewAgentSkillIndex()
	skills := idx.ListSkills()
	if len(skills) == 0 {
		t.Fatal("expected seeded skills")
	}
	found := false
	for _, s := range skills {
		if s.ID == "discord_monitor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected discord_monitor skill to be present")
	}
}

func TestAgentSkillIndex_SearchTools_Basic(t *testing.T) {
	idx := NewAgentSkillIndex()

	results := idx.SearchTools("send message discord", 5)
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'send message discord'")
	}

	// Best result should be the discord send tool
	top := results[0]
	if !strings.Contains(strings.ToLower(top.Tool.Name), "discord") ||
		!strings.Contains(strings.ToLower(top.Tool.Description), "message") {
		t.Errorf("top result did not look like discord send: %+v", top.Tool)
	}
	if top.Score < 0.3 {
		t.Errorf("expected reasonably high score, got %f", top.Score)
	}
}

func TestAgentSkillIndex_SearchTools_Semanticish(t *testing.T) {
	idx := NewAgentSkillIndex()

	// Natural language query that doesn't contain exact tool name
	results := idx.SearchTools("post something to chat on discord", 3)
	if len(results) == 0 {
		t.Fatal("expected results for natural language discord query")
	}

	foundDiscord := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.Tool.Name), "discord") {
			foundDiscord = true
			break
		}
	}
	if !foundDiscord {
		t.Error("semantic-ish search should still surface discord tools")
	}
}

func TestAgentSkillIndex_SearchTools_NoResults(t *testing.T) {
	idx := NewAgentSkillIndex()
	results := idx.SearchTools("completely unrelated quantum teleportation blockchain", 5)
	// We may get weak matches; just ensure it doesn't panic and returns something reasonable
	if len(results) > 5 {
		t.Error("should respect limit")
	}
}
