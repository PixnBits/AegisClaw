package main

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"AegisClaw/internal/transport/hubclient"
)

func TestGeneratePlanDoesNotDumpSystemPrompt(t *testing.T) {
	plan := generatePlan("tweak CSS padding", "main")
	if strings.Contains(plan, "You are the Project Manager") || strings.Contains(plan, "PASS") {
		t.Fatalf("fallback plan must not include the system prompt, got: %s", plan)
	}
	if !strings.Contains(plan, "main") {
		t.Fatalf("plan should mention the channel, got: %s", plan)
	}
}

func TestSanitizePMPostStripsSpeakAndPromptEcho(t *testing.T) {
	fallback := "Plan: assign Coder."
	got := sanitizePMPost("SPEAK\n@Coder fix the padding. @Tester verify.", fallback)
	if strings.HasPrefix(got, "SPEAK") || !strings.Contains(got, "@Coder") {
		t.Fatalf("expected stripped SPEAK body, got %q", got)
	}
	echo := getPMPrompt() + "\n\nStructured Plan:\n1. Analyze"
	if sanitizePMPost(echo, fallback) != fallback {
		t.Fatal("prompt echo must fall back")
	}
	if sanitizePMPost("PASS", fallback) != fallback {
		t.Fatal("PASS-only plan must fall back")
	}
	liveDump := "You are the Project Manager in AegisClaw's paranoid-isolated system. Untrusted components run in dedicated Firecracker microVM sandboxes. All communication is mediated by AegisHub with ACLs and signing."
	if !looksLikePromptEcho(liveDump) {
		t.Fatal("exact live #main regurgitation must count as prompt echo")
	}
	if sanitizePMPost(liveDump, fallback) != fallback {
		t.Fatal("live #main regurgitation must fall back, not be posted")
	}
}

func TestExtractRolesFromTextDoesNotSpawnCourtOnNoCourt(t *testing.T) {
	css := extractRolesFromText("- @Coder: tweak login-button padding.\n- @Tester: visual check.\n- No Court.")
	if len(css) != 2 || css[0] != "coder" || css[1] != "tester" {
		t.Fatalf("CSS No Court plan should ensure coder+tester only, got %v", css)
	}
	if roles := extractRolesFromText(generatePlan("The login button padding is 2px off. CSS only.", "main")); len(roles) != 2 {
		t.Fatalf("CSS fallback must not ensure CISO/Court, got %v", roles)
	}
	if roles := extractRolesFromText(generatePlan("I would like to plan a birthday party for a seven year old.", "main")); len(roles) != 0 {
		t.Fatalf("birthday must not ensure engineering roles, got %v", roles)
	}
	secret := extractRolesFromText("- Treat the key as compromised. Rotate it now.\n- @CISO: confirm rotation and log scrub.")
	if len(secret) != 1 || secret[0] != "ciso" {
		t.Fatalf("secret plan should ensure CISO only, got %v", secret)
	}
	if roles := extractRolesFromText("Court only for security, isolation, or architecture changes."); containsRole(roles, "ciso") || containsRole(roles, "architect") {
		t.Fatalf("constraint sentence must not spawn Court, got %v", roles)
	}
}

func containsRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func TestExtractGoalFromPayload(t *testing.T) {
	got := extractGoalFromPayload(map[string]interface{}{
		"channel":    "main",
		"channel_id": "main",
		"content":    "The login button padding is 2px off.",
		"goal":       "The login button padding is 2px off.",
	})
	if got != "The login button padding is 2px off." {
		t.Fatalf("got %q", got)
	}
}

type pmTestHub struct {
	posts []string
	roles []string
}

func (h *pmTestHub) Register(context.Context, string, ed25519.PublicKey, string) (*hubclient.RegisterResponse, error) {
	return &hubclient.RegisterResponse{AssignedID: "project-manager"}, nil
}
func (h *pmTestHub) Send(_ context.Context, msg hubclient.Message) (hubclient.Message, error) {
	p, _ := msg.Payload.(map[string]interface{})
	switch msg.Command {
	case "channel.post":
		if c, ok := p["content"].(string); ok {
			h.posts = append(h.posts, c)
		}
		return hubclient.Message{Command: "channel.posted"}, nil
	case "ensure.role":
		if r, ok := p["role"].(string); ok {
			h.roles = append(h.roles, r)
		}
		return hubclient.Message{Command: "response"}, nil
	default:
		return hubclient.Message{Command: "response"}, nil
	}
}
func (h *pmTestHub) Reply(context.Context, hubclient.Message) error { return nil }
func (h *pmTestHub) Close() error                                   { return nil }
func (h *pmTestHub) AssignedID() string                             { return "project-manager" }
func (h *pmTestHub) IsVsock() bool                                  { return false }
func (h *pmTestHub) Receive(context.Context) (hubclient.Message, error) {
	return hubclient.Message{}, nil
}
func (h *pmTestHub) TryReceive(context.Context, time.Duration) (hubclient.Message, bool, error) {
	return hubclient.Message{}, false, nil
}

func TestPMHumanChannelTurnUsesPlanPromptNotSystemDump(t *testing.T) {
	hub := &pmTestHub{}
	var prompt string
	llm := func(_ context.Context, p string) (string, error) {
		prompt = p
		return getPMPrompt(), nil
	}
	msg := hubclient.Message{
		Source:  "store",
		Command: "channel.turn",
		Payload: map[string]interface{}{
			"channel_id": "main",
			"since_seq":  1,
			"new_messages": []interface{}{
				map[string]interface{}{"from": "user", "content": "The login button padding is 2px off. CSS only."},
			},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	pmProcessChannelTurn(hub, msg, "project-manager", llm)
	if !strings.Contains(prompt, "Output ONLY the plan") {
		t.Fatalf("human turn must use plan prompt, got: %s", prompt)
	}
	if strings.Contains(prompt, "map[") {
		t.Fatalf("user goal must not be a Go map dump: %s", prompt)
	}
	if !strings.Contains(prompt, "login button padding") {
		t.Fatalf("plan prompt should include the user goal, got: %s", prompt)
	}
	if len(hub.posts) != 1 {
		t.Fatalf("expected one plan post, got %v", hub.posts)
	}
	if looksLikePromptEcho(hub.posts[0]) {
		t.Fatalf("system prompt echo was posted: %s", hub.posts[0])
	}
	if containsRole(hub.roles, "ciso") {
		t.Fatalf("CSS plan must not ensure CISO, roles=%v posts=%v", hub.roles, hub.posts)
	}
}

func TestSanitizePMChannelReplyDropsEmptyAck(t *testing.T) {
	if _, skip := sanitizePMChannelReply("SPEAK\nThe system status update is complete and no further action is required from the project manager."); !skip {
		t.Fatal("empty status ack must not post")
	}
	if _, skip := sanitizePMChannelReply("SPEAK\nThanks for the update — no new plan needed at this time."); !skip {
		t.Fatal("thanks-for-the-update ack must not post")
	}
	if _, skip := sanitizePMChannelReply("SPEAK\n@ProjectManager - the login padding task is still pending; Coder and Tester own it, but no new work has been posted."); !skip {
		t.Fatal("repeating still-pending recap must not post")
	}
	content, skip := sanitizePMChannelReply("SPEAK\nYes — Coder and Tester still own the 2px padding.")
	if skip || !strings.Contains(content, "Coder") {
		t.Fatalf("real @mention reply must post, got skip=%v content=%q", skip, content)
	}
}

func TestPMTurnSkipsOwnPlanAndSystemStatus(t *testing.T) {
	hub := &pmTestHub{}
	llm := func(context.Context, string) (string, error) {
		t.Fatal("must not call LLM on own plan / system status")
		return "", nil
	}
	msg := hubclient.Message{
		Source:  "store",
		Command: "channel.turn",
		Payload: map[string]interface{}{
			"channel_id": "css-v2",
			"since_seq":  3,
			"new_messages": []interface{}{
				map[string]interface{}{"from": "project-manager-css-v2", "content": "Plan for #css-v2:\n- @Coder: padding.\n- No Court."},
				map[string]interface{}{"from": "system", "content": "status: turns delivered to [project-manager]"},
			},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	pmProcessChannelTurn(hub, msg, "project-manager-css-v2", llm)
	if len(hub.posts) != 0 {
		t.Fatalf("own plan + system status must not post, got %v", hub.posts)
	}
}

func TestPMSpecialistTurnPASSDoesNotPost(t *testing.T) {
	hub := &pmTestHub{}
	llm := func(_ context.Context, _ string) (string, error) {
		return "PASS", nil
	}
	msg := hubclient.Message{
		Source:  "store",
		Command: "channel.turn",
		Payload: map[string]interface{}{
			"channel_id": "main",
			"since_seq":  1,
			"new_messages": []interface{}{
				map[string]interface{}{"from": "court-persona-senior-coder", "content": "I'll bump the button padding 2px."},
			},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	pmProcessChannelTurn(hub, msg, "project-manager", llm)
	if len(hub.posts) != 0 {
		t.Fatalf("PASS must not post, got %v", hub.posts)
	}
}

func TestPMDoesNotPostMonitoringOnPeerChannelPost(t *testing.T) {
	hub := &pmTestHub{}
	llm := func(context.Context, string) (string, error) {
		t.Fatal("should not call LLM for peer channel.post")
		return "", nil
	}
	msg := hubclient.Message{
		Command: "channel.post",
		Payload: map[string]interface{}{
			"channel_id": "main",
			"from":       "court-persona-tester",
			"content":    "snapshots green",
		},
	}
	pmProcessPlanningMessage(hub, msg, "project-manager", llm)
	if len(hub.posts) != 0 {
		t.Fatalf("must not post monitoring notes, got %v", hub.posts)
	}
}

func TestGeneratePlanDoesNotInviteCourtForCSSOrBirthday(t *testing.T) {
	css := generatePlan("The login button padding is 2px off. Please tweak CSS only.", "main")
	if !strings.Contains(css, "@Coder") || !strings.Contains(css, "No Court") {
		t.Fatalf("CSS fallback plan: %s", css)
	}
	if strings.Contains(css, "You are the Project Manager") {
		t.Fatalf("fallback must not dump system prompt: %s", css)
	}
	bday := generatePlan("I would like to plan a birthday party for a to-be seven year old boy.", "main")
	if strings.Contains(strings.ToLower(bday), "@coder") || strings.Contains(strings.ToLower(bday), "@ciso") {
		t.Fatalf("birthday fallback must not assign engineering roles: %s", bday)
	}
}
