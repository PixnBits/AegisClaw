package main

import (
	"context"
	"crypto/ed25519"
	"errors"
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
	for _, goal := range []string{
		"The login button padding is 2px off. CSS only.",
		"I would like to plan a birthday party for a seven year old.",
		"OAuth callback is ready but egress to accounts.google.com is denied.",
	} {
		if roles := extractRolesFromText(generatePlan(goal, "main")); len(roles) != 0 {
			t.Fatalf("keyword fallback must not assign roles for %q, got %v", goal, roles)
		}
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
	posts     []string
	roles     []string
	failPosts int
}

func (h *pmTestHub) Register(context.Context, string, ed25519.PublicKey, string) (*hubclient.RegisterResponse, error) {
	return &hubclient.RegisterResponse{AssignedID: "project-manager"}, nil
}
func (h *pmTestHub) Send(_ context.Context, msg hubclient.Message) (hubclient.Message, error) {
	p, _ := msg.Payload.(map[string]interface{})
	switch msg.Command {
	case "channel.post":
		if h.failPosts > 0 {
			h.failPosts--
			return hubclient.Message{}, errors.New("channel.post failed")
		}
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
	resetPlannedHumanGoals()
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
	if _, skip := sanitizePMChannelReply("SPEAK\n@ciso-egress-r2 I need to understand the specific codebase or file paths. @ProjectManager, can you provide the repository or file locations?"); !skip {
		t.Fatal("quoting a specialist path-ask back at them must not post")
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

func TestPMSpecialistProgressDoesNotCallLLM(t *testing.T) {
	hub := &pmTestHub{}
	llm := func(context.Context, string) (string, error) {
		t.Fatal("PM must not recap specialist progress")
		return "SPEAK\n@Coder owns padding.", nil
	}
	msg := hubclient.Message{
		Source:  "store",
		Command: "channel.turn",
		Payload: map[string]interface{}{
			"channel_id": "tune-css-9",
			"since_seq":  3,
			"new_messages": []interface{}{
				map[string]interface{}{"from": "coder-tune-css-9", "content": "I'll adjust the login button padding to fix the 2px offset issue."},
			},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	pmProcessChannelTurn(hub, msg, "project-manager-tune-css-9", llm)
	if len(hub.posts) != 0 {
		t.Fatalf("specialist ack must not produce a PM recap, got %v", hub.posts)
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

func TestGeneratePlanIsTopicAgnostic(t *testing.T) {
	css := generatePlan("The login button padding is 2px off. Please tweak CSS only.", "main")
	bday := generatePlan("I would like to plan a birthday party for a to-be seven year old boy.", "main")
	egress := generatePlan("We are stuck. OAuth callback is ready but egress to accounts.google.com is denied.", "main")
	if css != bday || css != egress {
		t.Fatalf("fallback must not switchboard on topic, css=%q bday=%q egress=%q", css, bday, egress)
	}
	if strings.Contains(css, "You are the Project Manager") || strings.Contains(css, "PASS") {
		t.Fatalf("fallback must not dump system prompt: %s", css)
	}
	if !strings.Contains(css, "main") {
		t.Fatalf("plan should mention the channel, got: %s", css)
	}
	lower := strings.ToLower(css)
	if strings.Contains(lower, "@coder") || strings.Contains(lower, "@ciso") || strings.Contains(lower, "@tester") {
		t.Fatalf("generic fallback must not assign roles, got: %s", css)
	}
	if strings.Contains(lower, "restate the goal") {
		t.Fatalf("fallback must not post instruction text as a plan: %s", css)
	}
}

func TestLooksLikePromptEchoKeepsRealPlans(t *testing.T) {
	good := "@Coder tweak the login button padding. Ask which repo and path first. @Tester visual check. No Court."
	if looksLikePromptEcho(good) {
		t.Fatalf("real plan must not count as echo: %s", good)
	}
	dump := "You are the Project Manager in AegisClaw's paranoid-isolated system. Untrusted components run in dedicated Firecracker microVM sandboxes."
	if !looksLikePromptEcho(dump) {
		t.Fatal("architecture dump must still count as echo")
	}
}

func humanTurn(chID, content string) hubclient.Message {
	return hubclient.Message{
		Source:  "store",
		Command: "channel.turn",
		Payload: map[string]interface{}{
			"channel_id": chID,
			"since_seq":  0,
			"new_messages": []interface{}{
				map[string]interface{}{"from": "user", "content": content},
			},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

func userGoalMsg(chID, content string) hubclient.Message {
	return hubclient.Message{
		Source:  "aegis-cli-internal",
		Command: "user.goal",
		Payload: map[string]interface{}{
			"channel":    chID,
			"channel_id": chID,
			"goal":       content,
			"content":    content,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

func TestPMPlansOnceWhenUserGoalThenHumanTurn(t *testing.T) {
	resetPlannedHumanGoals()
	hub := &pmTestHub{}
	var prompts []string
	llm := func(_ context.Context, p string) (string, error) {
		prompts = append(prompts, p)
		if strings.Contains(p, "Output ONLY the plan") {
			return "@Coder take the assignment. Ask for the repo if missing.", nil
		}
		return "PASS", nil
	}
	goal := "Ship a small docs fix in the existing repo."
	pmProcessPlanningMessage(hub, userGoalMsg("once-a", goal), "project-manager-once-a", llm)
	pmProcessChannelTurn(hub, humanTurn("once-a", goal), "project-manager-once-a", llm)
	if len(hub.posts) != 1 {
		t.Fatalf("expected one plan post, got %d %v", len(hub.posts), hub.posts)
	}
	if len(prompts) != 2 {
		t.Fatalf("claimed human turn should fall through to channel LLM, prompts=%d", len(prompts))
	}
	if !strings.Contains(prompts[0], "Output ONLY the plan") {
		t.Fatalf("first call must be plan prompt, got %s", prompts[0])
	}
	if !strings.Contains(prompts[1], "PASS or SPEAK") && !strings.Contains(prompts[1], "PASS") {
		t.Fatalf("second call must be channel prompt, got %s", prompts[1])
	}
	if strings.Contains(prompts[1], "Output ONLY the plan") {
		t.Fatal("already-claimed human turn must not use the plan prompt")
	}
}

func TestPMPlansOnceWhenHumanTurnThenUserGoal(t *testing.T) {
	resetPlannedHumanGoals()
	hub := &pmTestHub{}
	calls := 0
	llm := func(context.Context, string) (string, error) {
		calls++
		return "@Tester verify once there is a path.", nil
	}
	goal := "Confirm the health endpoint still returns 200."
	pmProcessChannelTurn(hub, humanTurn("once-b", goal), "project-manager-once-b", llm)
	pmProcessPlanningMessage(hub, userGoalMsg("once-b", goal), "project-manager-once-b", llm)
	if calls != 1 {
		t.Fatalf("expected one LLM plan, got %d", calls)
	}
	if len(hub.posts) != 1 {
		t.Fatalf("expected one plan post, got %d %v", len(hub.posts), hub.posts)
	}
}

func TestPMPlansAgainForDifferentHumanGoal(t *testing.T) {
	resetPlannedHumanGoals()
	hub := &pmTestHub{}
	calls := 0
	llm := func(context.Context, string) (string, error) {
		calls++
		return "Plan next step.", nil
	}
	pmProcessPlanningMessage(hub, userGoalMsg("once-c", "First distinct goal about logs."), "project-manager-once-c", llm)
	pmProcessChannelTurn(hub, humanTurn("once-c", "Second distinct goal about metrics."), "project-manager-once-c", llm)
	if calls != 2 || len(hub.posts) != 2 {
		t.Fatalf("different goals must each plan, calls=%d posts=%d", calls, len(hub.posts))
	}
}

func TestClaimHumanGoalFailsClosedOnEmpty(t *testing.T) {
	resetPlannedHumanGoals()
	if claimHumanGoal("", "a goal") {
		t.Fatal("empty channel must fail closed")
	}
	if claimHumanGoal("ch", "") {
		t.Fatal("empty goal must fail closed")
	}
	if claimHumanGoal("ch", "   ") {
		t.Fatal("whitespace goal must fail closed")
	}
}

func TestPMFallbackThenSameTextPlansAgain(t *testing.T) {
	resetPlannedHumanGoals()
	hub := &pmTestHub{}
	calls := 0
	llm := func(context.Context, string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("llm down")
		}
		return "@Coder take the next step. Ask for the repo if missing.", nil
	}
	goal := "Add a health check to the existing service."
	pmProcessPlanningMessage(hub, userGoalMsg("once-fb", goal), "project-manager-once-fb", llm)
	if len(hub.posts) != 1 {
		t.Fatalf("fallback should post, got %d", len(hub.posts))
	}
	if !strings.Contains(hub.posts[0], "Could not draft a plan") {
		t.Fatalf("expected honest fallback, got %q", hub.posts[0])
	}
	pmProcessPlanningMessage(hub, userGoalMsg("once-fb", goal), "project-manager-once-fb", llm)
	if calls != 2 || len(hub.posts) != 2 {
		t.Fatalf("resend after fallback must plan again, calls=%d posts=%d", calls, len(hub.posts))
	}
}

func TestPMPostErrorThenSameTextPlansAgain(t *testing.T) {
	resetPlannedHumanGoals()
	hub := &pmTestHub{failPosts: 1}
	calls := 0
	llm := func(context.Context, string) (string, error) {
		calls++
		return "@Tester verify once there is a path.", nil
	}
	goal := "Check the existing deploy script still runs."
	pmProcessPlanningMessage(hub, userGoalMsg("once-pe", goal), "project-manager-once-pe", llm)
	if len(hub.posts) != 0 {
		t.Fatalf("failed post must not record a plan, got %v", hub.posts)
	}
	pmProcessPlanningMessage(hub, userGoalMsg("once-pe", goal), "project-manager-once-pe", llm)
	if calls != 2 || len(hub.posts) != 1 {
		t.Fatalf("resend after post error must plan again, calls=%d posts=%d", calls, len(hub.posts))
	}
}

func TestClaimedHumanTurnFallsThroughToChannelPrompt(t *testing.T) {
	resetPlannedHumanGoals()
	hub := &pmTestHub{}
	var prompts []string
	llm := func(_ context.Context, p string) (string, error) {
		prompts = append(prompts, p)
		if strings.Contains(p, "Output ONLY the plan") {
			return "Plan: owners still have it.", nil
		}
		return "PASS", nil
	}
	goal := "Document the existing retry budget in the README."
	pmProcessChannelTurn(hub, humanTurn("once-ft", goal), "project-manager-once-ft", llm)
	pmProcessChannelTurn(hub, humanTurn("once-ft", goal), "project-manager-once-ft", llm)
	if len(hub.posts) != 1 {
		t.Fatalf("no second plan post, got %d %v", len(hub.posts), hub.posts)
	}
	if len(prompts) < 2 {
		t.Fatal("second human turn must call the channel prompt LLM")
	}
	last := prompts[len(prompts)-1]
	if strings.Contains(last, "Output ONLY the plan") {
		t.Fatal("already-claimed turn must not use the plan prompt")
	}
	if !strings.Contains(last, "PASS") && !strings.Contains(last, "SPEAK") {
		t.Fatalf("channel prompt missing PASS/SPEAK, got %s", last)
	}
}
