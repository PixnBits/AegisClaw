package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"AegisClaw/internal/agent"
	"AegisClaw/internal/agent/loop"
	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/channelfacilitator"
	"AegisClaw/internal/collab"
	"AegisClaw/internal/timing"
	"AegisClaw/internal/transport/hubclient"
	"AegisClaw/internal/workspace"
	"github.com/spf13/cobra"
)

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

var hubSocket = "~/.aegis/hub.sock"

var loadedWorkspace *workspace.Context

func init() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocket = env
	}
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

func getBuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		version := info.Main.Version
		if version == "" || version == "(devel)" {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
					return setting.Value[:7]
				}
			}
			return "dev"
		}
		return version
	}
	return "unknown"
}

func getPMPrompt() string {
	custom := ""
	if loadedWorkspace != nil {
		if loadedWorkspace.SOUL != "" {
			custom += "Core values and soul for this system: " + loadedWorkspace.SOUL + ". "
		}
		if loadedWorkspace.AGENTS != "" {
			custom += "Custom agent/PM instructions: " + loadedWorkspace.AGENTS + ". "
		}
	}

	// Shared system context for the Project Manager — mirrors the Court personas so the orchestrator
	// understands the full architecture and can delegate, monitor, and escalate effectively.
	systemContext := "You are the Project Manager in AegisClaw's paranoid-isolated system. Untrusted components run in dedicated Firecracker microVM sandboxes. All communication is mediated by AegisHub with ACLs and signing. LLM calls go through Network Boundary. Persistent state lives in Store VM; per-agent context in Memory VM. Skills/tools are discovered via tool.search after Court review and Builder VM implementation. Collaboration uses turn-based channel.turn with relevance_anchors and Store context tools (get_relevant_since / get_messages). You orchestrate via ensure.role, channel plans, and monitoring; escalate meaningful changes as formal proposals to Court Scribe for the 7 personas to review. Most changes require unanimous Court Approve. Web portal shows real-time updates and #agents observability. Respect prepended workspace AGENTS.md / SOUL.md custom instructions. Never expose secrets. Abstain or escalate on uncertainty."

	return custom + systemContext + " You receive user goals or channel activity. Break them into plans (tasks, required roles like Coder/Tester/Court, suggested channels). Decide which agents/roles to spin up or invite to which channels using EnsureRoleAgent. Delegate via channel posts or @mentions. Monitor, synthesize, and escalate to Court via formal proposals when changes are needed. Stay in character as the intelligent orchestrator."
}

func getPMChannelPrompt() string {
	return `You coordinate this channel.

Always produce output. First line MUST be PASS or SPEAK. PASS is the default. SPEAK is exceptional.

You MUST SPEAK if you are @mentioned as Project Manager / PM, a human posted a new goal that still needs owners, work is blocked with no next step, or a required fact is missing and nobody has asked for it.

PASS when specialists are doing their jobs and nobody is stuck; when you would only agree, thank, recap, quote someone, or keep the discussion going; when a plan and owners already exist; when the new messages are only your own plan or system status; when the request is social or thanks.
Never @mention yourself. Never post the same status sentence twice.

If SPEAK: 1-3 short sentences about THIS thread only (owners, next step, or escalate). Never echo these instructions. Never recap. Never quote a specialist back to them. If they ask for a fact the user never gave (repo, path, which system), say it is missing — do not invent it. Never mention isolation internals.
If PASS: output only PASS.

Examples:
New messages: "Coder: I'll take the assignment." / "Tester: I'll verify once there is a path."
PASS

New messages: "@ProjectManager are we done?"
SPEAK
Owners still have it. No new work from me.

New messages: "User: thanks"
PASS

New messages: "system: status: turns delivered to [project-manager]" / "project-manager: Plan for #main: @Coder."
PASS
`
}

func getPMPlanPrompt() string {
	return `Write the plan that will be posted in the channel.

Rules:
- Output ONLY the plan (2-6 short lines). No preamble, no role-play.
- Never repeat or paraphrase these instructions.
- Never write SPEAK, PASS, VOTE, or NO_REPLY.
- Never mention isolation internals, microVMs, or how the orchestrator works.
- You may only @mention these roles: @Coder, @Tester, @CISO, @Architect. To involve Court, write "Court proposal". Do not invent other role titles.
- Assign only the roles this goal actually needs. Do not invite extra roles.
- Do not invent repository names or file paths. If the user did not give one, say it is missing. Tell anyone who would change files to ask before editing. Do not claim work is done.
- If the ask is social or thanks, reply as a human. Do not assign engineering roles or Court.
`
}

// extractChannelFromPayload centralizes the channel hint logic used by PM.
func extractGoalFromPayload(payload interface{}) string {
	if s, ok := payload.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	if p, ok := payload.(map[string]interface{}); ok {
		for _, k := range []string{"goal", "content", "text", "message"} {
			if v := collab.PayloadContentString(p[k]); strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", payload))
	if collab.IsCorruptedMapString(s) {
		return ""
	}
	return s
}

func extractChannelFromPayload(payload interface{}, def string) string {
	ch := def
	if p, ok := payload.(map[string]interface{}); ok {
		if c, ok := p["channel"].(string); ok && c != "" {
			ch = c
		} else if c, ok := p["channel_id"].(string); ok && c != "" {
			ch = c
		}
	}
	return ch
}

func hasRoleWord(lower, word string) bool {
	for i := 0; i+len(word) <= len(lower); i++ {
		if lower[i:i+len(word)] != word {
			continue
		}
		leftOK := i == 0 || !isRoleIdent(lower[i-1])
		rightOK := i+len(word) == len(lower) || !isRoleIdent(lower[i+len(word)])
		if leftOK && rightOK {
			return true
		}
	}
	return false
}

func isRoleIdent(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-'
}

func extractRolesFromText(text string) []string {
	lower := strings.ToLower(text)
	var roles []string
	add := func(role string) {
		for _, r := range roles {
			if r == role {
				return
			}
		}
		roles = append(roles, role)
	}
	if hasRoleWord(lower, "coder") {
		add("coder")
	}
	if hasRoleWord(lower, "tester") {
		add("tester")
	}
	if hasRoleWord(lower, "ciso") {
		add("ciso")
	}
	if hasRoleWord(lower, "security-architect") || hasRoleWord(lower, "secarch") {
		add("security-architect")
	} else if hasRoleWord(lower, "architect") {
		add("architect")
	}
	if hasRoleWord(lower, "efficiency") {
		add("efficiency")
	}
	if strings.Contains(lower, "user-advocate") || strings.Contains(lower, "user advocate") {
		add("user-advocate")
	}
	noCourt := strings.Contains(lower, "no court") ||
		strings.Contains(lower, "not invite court") ||
		strings.Contains(lower, "don't invite court") ||
		strings.Contains(lower, "do not invite court")
	if !noCourt && (strings.Contains(lower, "court scribe") || strings.Contains(lower, "court proposal") || strings.Contains(lower, "invite court")) {
		add("ciso")
	}
	return roles
}

func generatePlan(_, chID string) string {
	return "Plan for #" + chID + ":\n- Could not draft a plan this turn. Please resend the goal.\n"
}

func truncateForLog(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func looksLikePromptEcho(s string) bool {
	_, ok := promptEchoNeedle(s)
	return ok
}

func promptEchoNeedle(s string) (string, bool) {
	lower := strings.ToLower(s)
	needles := []string{
		"first line must be pass",
		"first line must be speak",
		"paranoid-isolated",
		"you are the project manager in aegisclaw",
		"you are aegisclaw's project manager",
		"untrusted components run",
		"ensureroleagent",
		"aegishub",
		"firecracker",
		"store vm",
		"network boundary",
		"stay in character as the intelligent orchestrator",
		"structured plan:",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return n, true
		}
	}
	if len(s) > 800 {
		return "too_long", true
	}
	return "", false
}

// sanitizePMPost cleans LLM output before channel.post. Plans must never dump the system prompt
// or leave SPEAK/PASS control tokens in the visible message.
func sanitizePMPost(raw, fallback string) string {
	s := collab.StripThinkTags(strings.TrimSpace(raw))
	if s == "" || looksLikePromptEcho(s) {
		return fallback
	}
	if content, skip := collab.NormalizeChannelLLMReply(s); !skip {
		if looksLikePromptEcho(content) || strings.TrimSpace(content) == "" {
			return fallback
		}
		return content
	}
	// Missing SPEAK/PASS (typical for a plan) — keep body unless it is a control-only PASS.
	first, rest, _ := strings.Cut(s, "\n")
	tok := strings.ToUpper(strings.TrimSpace(strings.Trim(first, "`*_ ")))
	tok = strings.TrimRight(tok, ".!:")
	switch tok {
	case "PASS", "NO_REPLY", "NOREPLY", "SILENT", "SKIP":
		return fallback
	case "SPEAK", "REPLY":
		body := strings.TrimSpace(rest)
		if body == "" || looksLikePromptEcho(body) {
			return fallback
		}
		return body
	}
	return s
}

func looksLikeEmptyPMAck(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return false
	}
	for _, n := range []string{
		"no further action",
		"no new plan needed",
		"system status update is complete",
		"thanks for the update",
		"no action is required from the project manager",
		"no further action is required from the project manager",
		"still pending",
		"no new work has been posted",
		"no new work from me",
		"can you provide the repository",
		"can you provide the specific repository",
		"i need to understand the specific codebase",
	} {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

func sanitizePMChannelReply(raw string) (content string, skip bool) {
	s := collab.StripThinkTags(strings.TrimSpace(raw))
	if s == "" || looksLikePromptEcho(s) {
		return "", true
	}
	content, skip = collab.NormalizeChannelLLMReply(s)
	if skip || looksLikePromptEcho(content) || looksLikeEmptyPMAck(content) {
		return "", true
	}
	return content, false
}

func pmTurnFrom(m map[string]interface{}) string {
	if s, ok := m["from"].(string); ok {
		return s
	}
	return ""
}

// pmBatchIsSelfOrSystem reports batches that must not produce a follow-up PM post
// (own plan, system status lines, empty). Mentions and other posters still go to the LLM.
func pmBatchHasHuman(msgs []map[string]interface{}) bool {
	for _, m := range msgs {
		if collab.IsHumanPoster(pmTurnFrom(m)) {
			return true
		}
	}
	return false
}

func pmBatchLooksBlocked(msgs []map[string]interface{}) bool {
	for _, m := range msgs {
		lower := strings.ToLower(collab.PayloadContentString(m["content"]))
		for _, n := range []string{"stuck", "blocked", "cannot proceed", "denied", "need a court", "need court"} {
			if strings.Contains(lower, n) {
				return true
			}
		}
	}
	return false
}

func pmBatchIsSelfOrSystem(uniqueSource string, msgs []map[string]interface{}) bool {
	if len(msgs) == 0 {
		return true
	}
	for _, m := range msgs {
		from := pmTurnFrom(m)
		if from == "" || from == "system" || collab.IsSelfPost(uniqueSource, from) {
			continue
		}
		content := collab.PayloadContentString(m["content"])
		if collab.IsHumanPoster(from) {
			return false
		}
		if collab.IsMentioned(uniqueSource, content) || collab.IsMentioned("project-manager", content) {
			return false
		}
		return false
	}
	return true
}

// pmProcessPlanningMessage runs LLM planning, channel.post, and ensure.role delegation.
// Must not run synchronously inside the Receive loop for hub RPC-delivered user.goal
// (see user.goal case: immediate Reply + goroutine).
func pmProcessPlanningMessage(hcl hubclient.Client, msg hubclient.Message, uniqueSource string, realLLM agent.LLMCallFunc) {
	payloadStr := fmt.Sprintf("%v", msg.Payload)
	goal := extractGoalFromPayload(msg.Payload)
	chID := extractChannelFromPayload(msg.Payload, "plan-demo")

	if strings.Contains(payloadStr, uniqueSource) && msg.Command != "user.goal" {
		return
	}

	if msg.Command == "channel.post" {
		from := ""
		if p, ok := msg.Payload.(map[string]interface{}); ok {
			if f, ok := p["from"].(string); ok {
				from = f
			}
		}
		if from != uniqueSource {
			return
		}
	}

	var plan string
	if goal == "" {
		goal = payloadStr
	}
	fallback := generatePlan(goal, chID)
	planPrompt := getPMPlanPrompt() + "\n\nUser goal: " + goal + "\n\nChannel: " + chID + "\n\nPlan:"
	llmPlan, err := realLLM(context.Background(), planPrompt)
	if err != nil || strings.TrimSpace(llmPlan) == "" {
		log.Printf("PM: LLM plan gen failed (%v), using honest fallback", err)
		plan = fallback
	} else {
		plan = sanitizePMPost(llmPlan, fallback)
		needle, echoed := promptEchoNeedle(llmPlan)
		log.Printf("PM: plan model=%s raw_chars=%d posted_chars=%d fallback=%v echo=%s raw=%q",
			bootargs.PMModel(agent.DefaultPMModel), len(llmPlan), len(plan), plan == fallback, needle, truncateForLog(llmPlan, 400))
		if echoed && plan == fallback {
			log.Printf("PM: sanitizer dropped raw plan as echo")
		}
	}
	postMsg := hubclient.Message{
		Source:      uniqueSource,
		Destination: "store",
		Command:     "channel.post",
		Payload: map[string]interface{}{
			"channel_id": chID,
			"from":       uniqueSource,
			"content":    plan,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := hcl.Send(context.Background(), postMsg); err != nil {
		log.Printf("pm: channel.post to store failed (ACL?): %v", err)
	} else {
		fmt.Printf("PM: posted plan to channel %s\n", chID)
	}

	rolesToEnsure := extractRolesFromText(plan)
	for _, r := range rolesToEnsure {
		ensureMsg := hubclient.Message{
			Source:      uniqueSource,
			Destination: "daemon-orchestrator",
			Command:     "ensure.role",
			Payload: map[string]interface{}{
				"role":    r,
				"channel": chID,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if _, err := hcl.Send(context.Background(), ensureMsg); err != nil {
			log.Printf("pm: ensure.role for %s failed (ACL or receiver?): %v", r, err)
		} else {
			fmt.Printf("PM: sent ensure.role for %s in channel %s\n", r, chID)
		}
	}

	lowerPlan := strings.ToLower(plan)
	if strings.Contains(lowerPlan, "ciso") || strings.Contains(lowerPlan, "security") {
		_, _ = hcl.Send(context.Background(), hubclient.Message{
			Source:      uniqueSource,
			Destination: "store",
			Command:     "channel.add_member",
			Payload: map[string]interface{}{
				"channel_id": chID,
				"role":       "court-persona-ciso",
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}

}

// pmProcessChannelActivity handles delivered channel activity; agents decide whether to reply.
func pmProcessChannelActivity(hcl hubclient.Client, msg hubclient.Message, uniqueSource string, realLLM agent.LLMCallFunc) {
	payload, _ := msg.Payload.(map[string]interface{})
	chID, _ := payload["channel_id"].(string)
	from, _ := payload["from"].(string)
	userContent := collab.PayloadContentString(payload["content"])
	if chID == "" {
		chID = "main"
	}

	collab.Tracef("project-manager", "channel.activity.recv", "ch=%s from=%s", chID, from)

	shouldDeliver, reason := collab.ShouldRespondToActivity(uniqueSource, from, userContent)
	if !shouldDeliver {
		_ = hcl.Reply(context.Background(), hubclient.Message{
			Source:      uniqueSource,
			Destination: msg.Source,
			Command:     "response",
			Payload: map[string]interface{}{
				"status":     "ignored",
				"reason":     string(reason),
				"channel_id": chID,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	_ = hcl.Reply(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: msg.Source,
		Command:     "response",
		Payload: map[string]interface{}{
			"status":     "delivered",
			"reason":     string(reason),
			"channel_id": chID,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	// Inline on hubclient connection — see court-persona processChannelActivity (no goroutine).
	prompt := getPMChannelPrompt() + "\n\nA user asked in channel " + chID + ":\n" + userContent +
		"\n\nFirst line must be PASS or SPEAK. If you are @mentioned or a new goal needs owners, SPEAK. Otherwise PASS."
	llmReply, err := realLLM(context.Background(), prompt)
	if err != nil {
		log.Printf("PM: channel reply LLM failed (not posting canned text): %v", err)
		collab.Tracef("project-manager", "channel.reply.skip", "ch=%s err=%v", chID, err)
		return
	}
	trimmed, skip := sanitizePMChannelReply(llmReply)
	if skip {
		fmt.Printf("PM: chose not to reply in %s\n", chID)
		collab.Tracef("project-manager", "channel.reply.skip", "ch=%s reason=no_reply", chID)
		return
	}
	postCtx, postCancel := context.WithTimeout(context.Background(), 90*time.Second)
	_, postErr := hcl.Send(postCtx, hubclient.Message{
		Source:      uniqueSource,
		Destination: "store",
		Command:     "channel.post",
		Payload: map[string]interface{}{
			"channel_id": chID,
			"from":       uniqueSource,
			"content":    trimmed,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	postCancel()
	if postErr != nil {
		log.Printf("PM: channel.post failed: %v", postErr)
		collab.Tracef("project-manager", "channel.post.fail", "ch=%s err=%v", chID, postErr)
		return
	}
	collab.Tracef("project-manager", "channel.post.ok", "ch=%s len=%d", chID, len(trimmed))
	fmt.Printf("PM: posted channel reply to %s (%s)\n", chID, reason)
}

func pmProcessChannelTurn(hcl hubclient.Client, msg hubclient.Message, uniqueSource string, realLLM agent.LLMCallFunc) {
	turn, ok := collab.ParseTurnPayload(msg.Payload)
	if !ok {
		return
	}
	chID := turn.ChannelID
	collab.Tracef("project-manager", "channel.turn.recv", "ch=%s since=%d new=%d", chID, turn.SinceSeq, len(turn.NewMessages))

	_ = hcl.Reply(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: msg.Source,
		Command:     "response",
		Payload: map[string]interface{}{
			"status": "delivered", "reason": "turn", "channel_id": chID,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	batchText := collab.FormatTurnMessages(turn.NewMessages)
	if pmBatchIsSelfOrSystem(uniqueSource, turn.NewMessages) {
		collab.Tracef("project-manager", "channel.turn.skip", "ch=%s reason=self_or_system", chID)
		return
	}
	// Human goal / plan trigger: run full planning path when turn includes user content.
	for _, m := range turn.NewMessages {
		from := ""
		if s, ok := m["from"].(string); ok {
			from = s
		}
		content := collab.PayloadContentString(m["content"])
		if collab.IsHumanPoster(from) && content != "" {
			planMsg := hubclient.Message{
				Source:      msg.Source,
				Destination: uniqueSource,
				Command:     "user.goal",
				Payload: map[string]interface{}{
					"channel":    chID,
					"channel_id": chID,
					"content":    content,
					"goal":       content,
				},
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			pmProcessPlanningMessage(hcl, planMsg, uniqueSource, realLLM)
			return
		}
	}

	mentioned := collab.IsMentioned(uniqueSource, batchText) || collab.IsMentioned("project-manager", batchText)
	if !mentioned && !pmBatchHasHuman(turn.NewMessages) && !pmBatchLooksBlocked(turn.NewMessages) {
		collab.Tracef("project-manager", "channel.turn.skip", "ch=%s reason=specialist_progress", chID)
		return
	}
	prompt := getPMChannelPrompt() + "\n\nChannel turn in " + chID + ":\n" + batchText
	if mentioned {
		prompt += "\n\nYou were directly @mentioned. First line MUST be SPEAK. One sentence is enough if no new plan is needed."
	} else {
		prompt += "\n\nYou were not @mentioned. PASS is the default. SPEAK if a new goal still needs owners, work is blocked with no next step, or Court escalation is missing. If PASS, output only PASS."
	}
	llmReply, err := realLLM(context.Background(), prompt)
	if err != nil {
		collab.Tracef("project-manager", "channel.turn.reply.skip", "ch=%s err=%v", chID, err)
		return
	}
	trimmed, skip := sanitizePMChannelReply(llmReply)
	if skip {
		return
	}
	postCtx, postCancel := context.WithTimeout(context.Background(), 90*time.Second)
	_, postErr := hcl.Send(postCtx, hubclient.Message{
		Source:      uniqueSource,
		Destination: "store",
		Command:     "channel.post",
		Payload: map[string]interface{}{
			"channel_id": chID,
			"from":       uniqueSource,
			"content":    trimmed,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	postCancel()
	if postErr != nil {
		collab.Tracef("project-manager", "channel.turn.post.fail", "ch=%s err=%v", chID, postErr)
		return
	}
	collab.Tracef("project-manager", "channel.turn.post.ok", "ch=%s len=%d", chID, len(trimmed))
}

func runProjectManager(cmd *cobra.Command, args []string) {
	timing.RecordPhase("main_entry")

	priv, pub, err := bootargs.LoadDistributedVMKey("project-manager")
	if err != nil {
		log.Printf("pm: %v — generating ephemeral key (dev only)", err)
		pub, priv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal("pm: failed to obtain key:", err)
		}
	}
	_ = pub

	wsCtx, wsErr := workspace.Load("")
	if wsErr != nil {
		log.Printf("pm: WARNING: %v (using defaults)", wsErr)
	} else if wsCtx.SOUL != "" || wsCtx.AGENTS != "" {
		log.Printf("pm: Loaded workspace customizations")
	}
	loadedWorkspace = wsCtx

	timing.RecordPhase("key_loaded")

	socket := expandPath(hubSocket)
	var hcl hubclient.Client
	if bootargs.UseHubVsock() {
		fmt.Println("project-manager: waiting for host hub bridge on vsock")
		hcl, err = hubclient.AcceptVsockHubBridge(hubclient.GuestHubBridgePort, priv)
	} else {
		hcl, err = hubclient.DialUnix(socket, priv)
	}
	if err != nil {
		log.Fatal("Failed to connect to AegisHub:", err)
	}
	defer hcl.Close()
	timing.RecordPhase("hub_dialed")

	uniqueSource := bootargs.ComponentID("project-manager")
	regResp, err := hcl.Register(context.Background(), uniqueSource, pub, getBuildVersion())
	if err != nil {
		log.Fatal("PM registration failed:", err)
	}
	fmt.Println("Project Manager registered as", uniqueSource, "assignedID=", regResp.AssignedID)
	timing.RecordPhase("register_complete")
	timing.WriteComponentReadySentinel()

	llmModel := bootargs.PMModel(agent.DefaultPMModel)
	realLLM := loop.NewRealLLMCaller(hcl, llmModel)
	if llmModel != agent.DefaultLLMModel {
		// Guest vsock/Ollama may fail a second model while the system default
		// (already used by Court) still works. Retry is logged, not silent.
		fallbackCaller := loop.NewRealLLMCaller(hcl, agent.DefaultLLMModel)
		primary := realLLM
		realLLM = func(ctx context.Context, prompt string) (string, error) {
			text, err := primary(ctx, prompt)
			if err == nil && strings.TrimSpace(text) != "" {
				return text, nil
			}
			log.Printf("PM: model %s failed (%v); retrying %s", llmModel, err, agent.DefaultLLMModel)
			return fallbackCaller(ctx, prompt)
		}
	}

	timing.RecordPhase("message_loop_ready")

	for {
		msg, err := hcl.Receive(context.Background())
		if err != nil {
			log.Println("pm: hub Receive error (continuing):", err)
			continue
		}

		fmt.Println("PM received:", msg.Command)

		switch msg.Command {
		case channelfacilitator.CmdTurn:
			pmProcessChannelTurn(hcl, msg, uniqueSource, realLLM)

		case "channel.activity", "channel.member_notify":
			pmProcessChannelActivity(hcl, msg, uniqueSource, realLLM)

		case "user.goal", "channel.post", "chat.message": // chat.message kept for legacy compat during transition; primary is user.goal via CLI `aegis pm goal` or future channel-triggered goals
			if msg.Command == "user.goal" {
				chID := extractChannelFromPayload(msg.Payload, "plan-demo")
				// Reply immediately so the CLI/hub RPC for user.goal completes without waiting
				// for LLM + channel.post. Planning must run on this connection without a
				// background goroutine: nested hcl.Send (llm.call, channel.post) shares the
				// hubclient decoder with Receive; if Receive runs concurrently it steals
				// llm.call.response and planning never posts to the channel (E2E empty messages).
				_ = hcl.Reply(context.Background(), hubclient.Message{
					Source:      uniqueSource,
					Destination: msg.Source,
					Command:     "response",
					Payload: map[string]interface{}{
						"status":  "accepted",
						"channel": chID,
						"note":    "planning async (LLM + channel.post + ensure.role)",
					},
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
				pmProcessPlanningMessage(hcl, msg, uniqueSource, realLLM)
				break
			}
			pmProcessPlanningMessage(hcl, msg, uniqueSource, realLLM)

		case "llm.call.response":
			// Orphaned RPC reply (should have been consumed by nested Send). Ignore.
			log.Printf("pm: ignoring stray %s (hubclient decoder race guard)", msg.Command)

		case "version", "get-version":
			_ = hcl.Reply(context.Background(), hubclient.Message{
				Source:      uniqueSource,
				Destination: msg.Source,
				Command:     "version",
				Payload:     map[string]string{"version": getBuildVersion()},
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "project-manager",
		Short: "Project Manager Agent (orchestrator for channels + roles)",
		Run:   runProjectManager,
	}
	rootCmd.Execute()
}
