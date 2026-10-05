package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
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
	systemContext := "You are the Project Manager in AegisClaw's paranoid-isolated system. Untrusted components run in dedicated Firecracker microVM sandboxes. All communication is mediated by AegisHub with ACLs and signing. LLM calls go through Network Boundary. Persistent state lives in Store VM; per-agent context in Memory VM. Skills/tools are discovered via tool.search after Court review and Builder VM implementation. Collaboration with specialists is direct: ensure each role, then assign the task by direct message. Do not broadcast plans with channel posts or hand off work with @mentions. One closing message may be posted for the human. Escalate meaningful changes as formal proposals to Court Scribe for the 7 personas to review. Most changes require unanimous Court Approve. Web portal shows real-time updates and #agents observability. Respect prepended workspace AGENTS.md / SOUL.md custom instructions. Never expose secrets. Abstain or escalate on uncertainty."

	return custom + systemContext + " You receive user goals. Break them into plans (tasks and the roles that goal needs, such as Coder or Tester). Spin those roles up and assign the work by direct message. Do not invite roles onto a channel for the assignment. Monitor the replies, synthesize one final answer, and escalate to Court via formal proposals when changes are needed. Stay in character as the intelligent orchestrator."
}

func getPMChannelPrompt() string {
	return `You coordinate this channel.

Always produce output. First line MUST be PASS or SPEAK. PASS is the default. SPEAK is exceptional.

You MUST SPEAK if you are @mentioned as Project Manager / PM, a human posted a new goal that still needs owners, work is blocked with no next step, or a required fact is missing and nobody has asked for it.

PASS when specialists are doing their jobs and nobody is stuck; when you would only agree, thank, recap, quote someone, or keep the discussion going; when a plan and owners already exist; when the new messages are only your own plan or system status; when the request is social or thanks.
Never @mention yourself. Never post the same status sentence twice.

If SPEAK: 1-3 short sentences about THIS thread only (owners, next step, or escalate). Assign work by naming a role the Project Manager will message directly. Do not hand off work with channel @mentions. Never echo these instructions. Never recap. Never quote a specialist back to them. If they ask for a fact the user never gave (repo, path, which system), say it is missing — do not invent it. Never mention isolation internals.
If PASS: output only PASS.

Examples:
New messages: "Coder: I'll take the assignment." / "Tester: I'll verify once there is a path."
PASS

New messages: "@ProjectManager are we done?"
SPEAK
Owners still have it. No new work from me.

New messages: "User: thanks"
PASS

New messages: "system: status: turns delivered to [project-manager]" / "project-manager: Coder has the task by direct message."
PASS
`
}

func getPMPlanPrompt() string {
	return `Write the plan you will use to assign work by direct message.

Rules:
- Output ONLY the plan (2-6 short lines). No preamble, no role-play.
- Never repeat or paraphrase these instructions.
- Never write SPEAK, PASS, VOTE, or NO_REPLY.
- Never mention isolation internals, microVMs, or how the orchestrator works.
- Name only these roles when someone must act: Coder, Tester, CISO, Architect. The Project Manager messages those roles directly. Do not hand off work with channel @mentions. To involve Court, write "Court proposal". Do not invent other role titles.
- Assign only the roles this goal actually needs. Do not invite extra roles.
- Do not invent repository names or file paths. If the user did not give one, say it is missing. Tell anyone who would change files to ask before editing. Do not claim work is done.
- Do not invent, punch, or apply network, firewall, or allowlist policy. Isolation and network-boundary changes need a Court proposal first. Do not assign anyone to write or apply a policy Court has not approved.
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

// plannedHumanGoals is process-local (not durable). A PM restart may plan the
// same text again. Values: goalInflight while LLM and direct messages are
// running, goalPosted after a successful non-fallback closing channel.post.
// Fallback and send errors delete the key so the same text may plan once more
// ("Please resend the goal."). The closing post is the only channel write:
// the plan itself when it names no roles, otherwise the synthesis.
const (
	goalInflight = "inflight"
	goalPosted   = "posted"
)

var plannedHumanGoals sync.Map

func normalizeHumanGoal(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func humanGoalKey(chID, goal string) (string, bool) {
	g := normalizeHumanGoal(goal)
	if chID == "" || g == "" {
		return "", false
	}
	return chID + "\x00" + g, true
}

func claimHumanGoal(chID, goal string) bool {
	key, ok := humanGoalKey(chID, goal)
	if !ok {
		log.Printf("PM: skip plan empty channel or goal ch=%q", chID)
		return false
	}
	_, loaded := plannedHumanGoals.LoadOrStore(key, goalInflight)
	return !loaded
}

func humanGoalIsPosted(chID, goal string) bool {
	key, ok := humanGoalKey(chID, goal)
	if !ok {
		return false
	}
	v, loaded := plannedHumanGoals.Load(key)
	return loaded && v == goalPosted
}

func markHumanGoalPosted(chID, goal string) {
	key, ok := humanGoalKey(chID, goal)
	if !ok {
		return
	}
	plannedHumanGoals.Store(key, goalPosted)
}

func releaseHumanGoal(chID, goal string) {
	key, ok := humanGoalKey(chID, goal)
	if !ok {
		return
	}
	plannedHumanGoals.Delete(key)
}

func resetPlannedHumanGoals() {
	plannedHumanGoals = sync.Map{}
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

// dmReply is one specialist's answer to a direct task.
type dmReply struct {
	Role    string
	AgentID string
	Content string
}

// pmPostClosing is the only channel write on the planning path. Intermediate
// specialist work stays on chat.message.
func pmPostClosing(hcl hubclient.Client, uniqueSource, chID, content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		content = generatePlan("", chID)
	}
	_, err := hcl.Send(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: "store",
		Command:     "channel.post",
		Payload: map[string]interface{}{
			"channel_id": chID,
			"from":       uniqueSource,
			"content":    content,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("pm: closing channel.post failed: %v", err)
		collab.Tracef("project-manager", "channel.post.fail", "ch=%s err=%v", chID, err)
		return false
	}
	collab.Tracef("project-manager", "channel.post.ok", "ch=%s len=%d closing=1", chID, len(content))
	fmt.Printf("PM: posted closing synthesis to channel %s\n", chID)
	return true
}

// pmEnsureRole starts the role agent without a channel. Omitting channel keeps
// the orchestrator from calling channel.add_member; an empty channel still spawns.
func pmEnsureRole(hcl hubclient.Client, uniqueSource, role string) (string, error) {
	resp, err := hcl.Send(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: "daemon-orchestrator",
		Command:     "ensure.role",
		Payload: map[string]interface{}{
			"role": role,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	id := ensuredAgentID(resp, role)
	if id == "" {
		return "", fmt.Errorf("ensure.role %s: %v", role, resp.Payload)
	}
	return id, nil
}

func ensuredAgentID(resp hubclient.Message, role string) string {
	if resp.Command == "error" {
		return ""
	}
	if p, ok := resp.Payload.(map[string]interface{}); ok {
		if errMsg, ok := p["error"].(string); ok && strings.TrimSpace(errMsg) != "" {
			return ""
		}
		if id, ok := p["id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	return role
}

func pmSendDirectTask(hcl hubclient.Client, uniqueSource, agentID, role, goal, plan string) (string, error) {
	// ensure.role returns when the VM is started, not when the guest has
	// registered. Retry only while the hub still says the agent is missing.
	deadline := time.Now().Add(30 * time.Second)
	delay := 250 * time.Millisecond
	var last error
	for {
		text, err := pmSendDirectTaskOnce(hcl, uniqueSource, agentID, role, goal, plan)
		if err == nil || !errors.Is(err, hubclient.ErrDestinationNotFound) || time.Now().After(deadline) {
			return text, err
		}
		last = err
		log.Printf("pm: chat.message to %s not registered yet: %v", agentID, err)
		time.Sleep(delay)
		if delay < 2*time.Second {
			delay *= 2
		}
		if time.Now().After(deadline) {
			return "", last
		}
	}
}

func pmSendDirectTaskOnce(hcl hubclient.Client, uniqueSource, agentID, role, goal, plan string) (string, error) {
	content := "The Project Manager assigned you this work. Reply directly to the Project Manager. Do not post to a channel.\n\nGoal: " + goal + "\n\nPlan:\n" + plan
	collab.Tracef("project-manager", "dm.task", "role=%s dest=%s", role, agentID)
	resp, err := hcl.Send(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: agentID,
		Command:     "chat.message",
		Payload: map[string]interface{}{
			"from":     uniqueSource,
			"reply_to": uniqueSource,
			"role":     role,
			"goal":     goal,
			"plan":     plan,
			"content":  content,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	if resp.Command == "error" {
		return "", fmt.Errorf("chat.message: %v", resp.Payload)
	}
	return dmReplyContent(resp), nil
}

func dmReplyContent(msg hubclient.Message) string {
	p, ok := msg.Payload.(map[string]interface{})
	if !ok {
		return ""
	}
	for _, k := range []string{"content", "text", "message"} {
		if s := strings.TrimSpace(collab.PayloadContentString(p[k])); s != "" {
			return s
		}
	}
	return ""
}

func directSynthesisPrompt(goal, plan string, replies []dmReply) string {
	var b strings.Builder
	b.WriteString("Write the final answer for the human who set the goal. Use only the goal, the plan, and the direct replies below. 2-5 short sentences. Do not repeat these instructions. Do not mention isolation internals. Do not use channel @mentions. Do not invent repository names, file paths, or finished work. If a reply is empty, say that role has not replied.\n\n")
	b.WriteString("Goal:\n")
	b.WriteString(goal)
	b.WriteString("\n\nPlan:\n")
	b.WriteString(plan)
	b.WriteString("\n\nDirect replies:\n")
	if len(replies) == 0 {
		b.WriteString("(none)\n")
	}
	for _, r := range replies {
		fmt.Fprintf(&b, "- %s: %s\n", r.Role, r.Content)
	}
	b.WriteString("\nFinal answer:")
	return b.String()
}

func synthesisFallback(replies []dmReply, plan string) string {
	if len(replies) == 0 {
		return plan
	}
	var b strings.Builder
	for i, r := range replies {
		if i > 0 {
			b.WriteString("\n")
		}
		text := strings.TrimSpace(r.Content)
		if text == "" {
			text = "(no reply)"
		}
		fmt.Fprintf(&b, "%s: %s", r.Role, text)
	}
	return b.String()
}

func sanitizeSynthesis(raw string, replies []dmReply, plan string) string {
	s := collab.StripThinkTags(strings.TrimSpace(raw))
	if s == "" || looksLikePromptEcho(s) {
		return synthesisFallback(replies, plan)
	}
	first, rest, _ := strings.Cut(s, "\n")
	tok := strings.ToUpper(strings.TrimSpace(strings.Trim(first, "`*_ ")))
	tok = strings.TrimRight(tok, ".!:")
	switch tok {
	case "PASS", "NO_REPLY", "NOREPLY", "SILENT", "SKIP":
		return synthesisFallback(replies, plan)
	case "SPEAK", "REPLY":
		body := strings.TrimSpace(rest)
		if body == "" || looksLikePromptEcho(body) {
			return synthesisFallback(replies, plan)
		}
		return body
	}
	return s
}

func synthesizeDirectReplies(goal, plan string, replies []dmReply, llm agent.LLMCallFunc) string {
	fallback := synthesisFallback(replies, plan)
	if llm == nil {
		return fallback
	}
	raw, err := llm(context.Background(), directSynthesisPrompt(goal, plan, replies))
	if err != nil || strings.TrimSpace(raw) == "" {
		log.Printf("PM: synthesis LLM failed (%v), using direct replies", err)
		return fallback
	}
	out := sanitizeSynthesis(raw, replies, plan)
	if strings.TrimSpace(out) == "" {
		return fallback
	}
	return out
}

// isSpecialistDMSource reports role agents that answer planning by hub reply.
// A late chat.message from one of them must not start another plan.
func isSpecialistDMSource(src string) bool {
	s := strings.ToLower(strings.TrimSpace(src))
	if s == "" || strings.HasPrefix(s, "project-manager") {
		return false
	}
	prefixes := []string{
		"coder", "tester", "architect", "ciso", "researcher",
		"security-architect", "efficiency", "user-advocate", "agent",
	}
	for _, p := range prefixes {
		if s == p || strings.HasPrefix(s, p+"-") {
			return true
		}
	}
	return false
}

// pmProcessPlanningMessage runs LLM planning, ensure.role, and direct tasks.
// The goal channel gets one closing post only. user.goal replies first, then
// calls this on the Receive goroutine (no extra background goroutine — nested
// Send shares the hubclient decoder).
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
	if !claimHumanGoal(chID, goal) {
		log.Printf("PM: skip duplicate plan ch=%s goal=%q", chID, truncateForLog(goal, 80))
		return
	}
	fallback := generatePlan(goal, chID)
	planPrompt := getPMPlanPrompt() + "\n\nUser goal: " + goal + "\n\nPlan:"
	llmPlan, err := realLLM(context.Background(), planPrompt)
	usedFallback := false
	if err != nil || strings.TrimSpace(llmPlan) == "" {
		log.Printf("PM: LLM plan gen failed (%v), using honest fallback", err)
		plan = fallback
		usedFallback = true
	} else {
		plan = sanitizePMPost(llmPlan, fallback)
		needle, echoed := promptEchoNeedle(llmPlan)
		log.Printf("PM: plan model=%s raw_chars=%d posted_chars=%d fallback=%v echo=%s raw=%q",
			bootargs.PMModel(agent.DefaultPMModel), len(llmPlan), len(plan), plan == fallback, needle, truncateForLog(llmPlan, 400))
		if echoed && plan == fallback {
			log.Printf("PM: sanitizer dropped raw plan as echo")
		}
		if plan == fallback {
			usedFallback = true
		}
	}
	rolesToEnsure := extractRolesFromText(plan)
	// No roles (or an unusable plan): one user-facing post, no specialist round.
	// Court members are not added on this path.
	if usedFallback || len(rolesToEnsure) == 0 {
		if !pmPostClosing(hcl, uniqueSource, chID, plan) {
			releaseHumanGoal(chID, goal)
			return
		}
		if usedFallback {
			releaseHumanGoal(chID, goal)
		} else {
			markHumanGoalPosted(chID, goal)
		}
		return
	}

	var replies []dmReply
	for _, r := range rolesToEnsure {
		id, err := pmEnsureRole(hcl, uniqueSource, r)
		if err != nil {
			log.Printf("pm: ensure.role for %s failed (ACL or receiver?): %v", r, err)
			replies = append(replies, dmReply{Role: r, Content: "(not started)"})
			continue
		}
		fmt.Printf("PM: sent ensure.role for %s (channel omitted)\n", r)
		text, err := pmSendDirectTask(hcl, uniqueSource, id, r, goal, plan)
		if err != nil {
			log.Printf("pm: chat.message to %s (%s) failed: %v", id, r, err)
			replies = append(replies, dmReply{Role: r, AgentID: id, Content: "(no reply)"})
			continue
		}
		if strings.TrimSpace(text) == "" {
			text = "(no reply)"
		}
		fmt.Printf("PM: direct task reply from %s (%s)\n", id, r)
		replies = append(replies, dmReply{Role: r, AgentID: id, Content: text})
	}

	closing := synthesizeDirectReplies(goal, plan, replies, realLLM)
	collab.Tracef("project-manager", "dm.closing", "ch=%s replies=%d", chID, len(replies))
	if !pmPostClosing(hcl, uniqueSource, chID, closing) {
		releaseHumanGoal(chID, goal)
		return
	}
	markHumanGoalPosted(chID, goal)
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
	// First distinct human text in this channel: planning. Already-posted
	// human text falls through to the SPEAK/PASS channel prompt (not silence).
	planned := false
	for _, m := range turn.NewMessages {
		from := ""
		if s, ok := m["from"].(string); ok {
			from = s
		}
		content := collab.PayloadContentString(m["content"])
		if !collab.IsHumanPoster(from) || content == "" {
			continue
		}
		if humanGoalIsPosted(chID, content) {
			continue
		}
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
		planned = true
		break
	}
	if planned {
		return
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

	wsCtx, wsErr := workspace.LoadForAgent("", "project-manager")
	if wsErr != nil {
		log.Printf("pm: WARNING: %v (using defaults)", wsErr)
	} else if wsCtx != nil && (wsCtx.SOUL != "" || wsCtx.AGENTS != "" || len(wsCtx.SETTINGS) > 0) {
		log.Printf("pm: Loaded workspace customizations")
	}
	if wsCtx != nil {
		_ = workspace.ValidateSettings(wsCtx.SETTINGS)
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
	if loadedWorkspace != nil && loadedWorkspace.SETTINGS != nil {
		if m, ok := loadedWorkspace.SETTINGS["model"].(string); ok && m != "" && !strings.EqualFold(m, "inherit") && !strings.EqualFold(m, "default") {
			llmModel = m
		}
	}
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

		case "user.goal", "channel.post", "chat.message": // chat.message from non-agents stays a legacy goal path; specialist replies are hub responses to planning
			if msg.Command == "chat.message" && isSpecialistDMSource(msg.Source) {
				log.Printf("PM: late direct reply from %s ignored (planning already collected the hub response)", msg.Source)
				break
			}
			if msg.Command == "user.goal" {
				chID := extractChannelFromPayload(msg.Payload, "plan-demo")
				// Reply immediately so the CLI/hub RPC for user.goal completes without waiting
				// for LLM + direct messages. Planning must run on this connection without a
				// background goroutine: nested hcl.Send (llm.call, chat.message, channel.post)
				// shares the hubclient decoder with Receive; if Receive runs concurrently it
				// steals llm.call.response and planning never finishes.
				_ = hcl.Reply(context.Background(), hubclient.Message{
					Source:      uniqueSource,
					Destination: msg.Source,
					Command:     "response",
					Payload: map[string]interface{}{
						"status":  "accepted",
						"channel": chID,
						"note":    "planning (LLM + direct messages + one closing post)",
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
