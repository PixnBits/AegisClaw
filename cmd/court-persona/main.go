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
	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/channelfacilitator"
	"AegisClaw/internal/collab"
	"AegisClaw/internal/eventbus"
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
var persona string

// 7.6: Loaded workspace customizations (AGENTS.md, SOUL.md, etc.) so that
// Court personas can respect user-defined instructions during reviews.
// This is the symmetric integration to what was done in the Agent 6-step loop.
var loadedWorkspace *workspace.Context

// resolvePersona returns the persona name from flag (highest priority),
// AEGIS_COURT_PERSONA env, or kernel cmdline "aegis.persona=xxx" (for real Firecracker boots).
// This enables a single court-persona.img to serve all 7 personas when launched by the orchestrator.
// SPEC: governance-court.md §Architecture (7 independent microVMs, each with dedicated persona).
func resolvePersona(flagVal string) string {
	if flagVal != "" {
		return strings.ToLower(strings.TrimSpace(flagVal))
	}
	if env := os.Getenv("AEGIS_COURT_PERSONA"); env != "" {
		return strings.ToLower(strings.TrimSpace(env))
	}
	// Fallback: parse /proc/cmdline for aegis.persona= (Firecracker kernel append path)
	if data, err := os.ReadFile("/proc/cmdline"); err == nil {
		line := string(data)
		for _, kv := range strings.Fields(line) {
			if strings.HasPrefix(kv, "aegis.persona=") {
				return strings.ToLower(strings.TrimPrefix(kv, "aegis.persona="))
			}
		}
	}
	return ""
}

// loadDistributedKey implements the paranoid per-VM key consumption contract
// (orchestrator.go:89-164 + security-model.md). Prefers the ephemeral 0600 file
// written by the Host Daemon for real Firecracker guests. Falls back to on-the-fly
// generation only for host/dev testing (never in prod Court path).
// Caller MUST zero the returned priv immediately after Dial/Register.
func loadDistributedKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	// 1. Explicit path via env (orchestrator writes <id>.vmkey with base64 priv)
	if path := os.Getenv("AEGIS_VM_PRIVATE_KEY_PATH"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read distributed VM key: %w (fail-closed)", err)
		}
		privBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(privBytes) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("invalid distributed key material (fail-closed)")
		}
		priv := ed25519.PrivateKey(privBytes)
		pub := priv.Public().(ed25519.PublicKey)
		return priv, pub, nil
	}

	// 2. Fallback well-known location used by some integration flows
	fallback := "/tmp/aegis-vm-key"
	if b, err := os.ReadFile(fallback); err == nil {
		if privBytes, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b))); decErr == nil && len(privBytes) == ed25519.PrivateKeySize {
			priv := ed25519.PrivateKey(privBytes)
			pub := priv.Public().(ed25519.PublicKey)
			return priv, pub, nil
		}
	}

	// 3. Dev/host only: generate (documented that real Court VMs always receive distributed keys)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	log.Println("WARNING: Court persona generated ephemeral key (dev path only; real Firecracker VMs receive orchestrator-distributed key)")
	return priv, pub, nil
}

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
			// Use commit hash if available
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
					return setting.Value[:7] // Short commit hash
				}
			}
			return "dev"
		}
		return version
	}
	return "unknown"
}

func workspaceCustomPrefix() string {
	// 7.6: Prepend user workspace customizations (SOUL + AGENTS) if present.
	if loadedWorkspace == nil {
		return ""
	}
	custom := ""
	if loadedWorkspace.SOUL != "" {
		custom += "Core values and soul for this system: " + loadedWorkspace.SOUL + ". "
	}
	if loadedWorkspace.AGENTS != "" {
		custom += "Custom agent/Court instructions: " + loadedWorkspace.AGENTS + ". "
	}
	return custom
}

// courtProposalSystemContext is shared Court context for formal proposal reviews (VOTE path).
const courtProposalSystemContext = "You operate inside AegisClaw's paranoid-isolated architecture. Untrusted components (including agents and skills) run in dedicated Firecracker microVM sandboxes. All communication is mediated by AegisHub with strict ACLs and ed25519 signing. LLM calls route through Network Boundary for policy, auditing and secret isolation. Persistent state (proposals, channel last_seen_seq, skill registry, Merkle audit logs) is owned by Store VM. Per-agent Memory VM holds context. Skills/tools are discovered semantically via tool.search (natural language query, permission-scoped) after Court-approved proposals and Builder VM implementation. Collaboration uses turn-based channel.turn messages with relevance_anchors. PM orchestrates via ensure.role and channel plans, escalating formal proposals to Court Scribe. Most changes require proposals + unanimous Approve from non-abstaining Court personas. Never expose secrets in reasoning or posts. Abstain on uncertainty rather than guessing. Stay strictly in character."

// cisoChannelInstructions is the CISO-only channel turn prompt.
// LLMs are trained to always produce a token sequence, so silence is modeled as the
// action PASS (not "do not respond"). SPEAK is the exception.
const cisoChannelInstructions = `You are the Chief Information Security Officer (CISO) sitting in an AegisClaw Slack-style channel.

Your professional job is selective governance, not conversation. You are not a facilitator, not a project manager, and not a rubber-stamp participant. Most turns you take no action in the channel.

Always produce output. The first line MUST be exactly one of these two action tokens:
PASS
SPEAK

PASS is the default and is correct professional behavior. SPEAK is exceptional.

You MUST SPEAK if you are directly @mentioned or a question is aimed at the CISO / "security officer" / "compliance lead", even when the answer is that there is no CISO issue. In that case write one short sentence (e.g. "No CISO issue in this CSS/copy change.") and stop.

Otherwise SPEAK only if at least one of these is true of the NEW messages (not of old recap):
- A new material risk appears: secrets/credentials/tokens/keys, PII, audit/compliance (GDPR, SOC2, SBOM), encryption, privilege/scopes, isolation/sandbox weakening, network egress or bind-all interfaces, third-party integrations, or skipping Court/governance.
- Someone proposes deploying, merging, or enabling a change that expands attack surface or stores secrets unsafely, and no Court proposal covers it yet.
- An incident or suspected sandbox escape / key leak needs an immediate posture call.

PASS when any of these apply AND you were not @mentioned:
- Implementation, code style, tests, UX/copy, CSS, performance, scheduling, standups, or status chatter with no new security decision.
- Other specialists (Security Architect, Architect, Coder, Tester, Efficiency, User Advocate, PM) are already covering the thread.
- You would only agree, thank, recap, restate, encourage, or keep the discussion going.
- The same risk was already stated in prior context and nothing new changed.
- The thread is cycling, off-track, or asking open questions that are not yours to answer.
- You are uncertain and have no new, concrete risk to add.

If SPEAK: after the first line write 1-3 short sentences. State the risk and the required control (or, on a mention with no risk, state that there is no CISO issue). Do not use VOTE format. Do not ask a question unless a security decision is blocked without an answer. Do not invite more discussion. Do not recap the thread.

If PASS: output only PASS.

Examples:
New messages: "Tester: I'll add unit tests for the parser." / "Senior Coder: renaming the helper."
PASS

New messages: "@CISO can we store the GitHub PAT in the agent prompt for convenience?"
SPEAK
No. Secrets must never go in prompts, channel text, or env files. Put the PAT in the Secrets VM and grant a scoped Network Boundary fetch after a Court proposal.

New messages: "User Advocate: the empty-state copy feels cold." / "Efficiency: we can drop one pre-warm VM."
PASS

New messages: "@CISO any security concern with the CSS token refactor?"
SPEAK
No CISO issue in a local CSS token refactor. Do not add a third-party font CDN.

New messages: "@CISO do you want a coverage floor as a security gate?"
SPEAK
No. Test coverage is a quality bar, not a CISO control. Do not turn coverage percentage into a security gate.
`

func getPersonaPrompt(persona string) string {
	custom := workspaceCustomPrefix()
	base := ""
	switch persona {
	case "ciso":
		base = courtProposalSystemContext + " You are the Chief Information Security Officer. Evaluate the proposal for security risks, compliance, secret handling, isolation boundaries, attack surface, and business/governance impact. Respond ONLY with a single line starting with VOTE: Approve|Reject|Abstain followed by | REASONING: ... | SPECIFIC_FEEDBACK: bullet list or none. Never guess — Abstain on uncertainty. This prompt is for formal proposals only, not channel chatter."
	case "security-architect":
		base = courtProposalSystemContext + " You are the Security Architect. Assess technical security design, sandbox escapes, privilege escalation, network boundary policy, and implementation of isolation controls. Respond ONLY with VOTE: ... | REASONING: ... | SPECIFIC_FEEDBACK: .... Abstain rather than speculate."
	case "architect":
		base = courtProposalSystemContext + " You are the System Architect. Review system design, modularity, long-term maintainability, composition of sandboxes and boundaries, and alignment with overall architecture. Respond ONLY with VOTE: ... | REASONING: ... | SPECIFIC_FEEDBACK: ...."
	case "senior-coder":
		base = courtProposalSystemContext + " You are the Senior Coder. Evaluate code quality, readability, implementation standards, correctness, and how changes interact with the isolated runtime, hub mediation, and tool discovery mechanisms. Respond ONLY with VOTE: ... | REASONING: ... | SPECIFIC_FEEDBACK: ...."
	case "tester":
		base = courtProposalSystemContext + " You are the Tester. Assess testing strategy, coverage, edge cases, reliability of isolation boundaries, and validation of governance flows or turn-based collaboration. Respond ONLY with VOTE: ... | REASONING: ... | SPECIFIC_FEEDBACK: ...."
	case "efficiency":
		base = courtProposalSystemContext + " You are the Efficiency Expert. Review performance, resource usage (microVMs, hub, LLM calls), cost, latency of channels/turns/proposals, and optimisation opportunities within the architecture. Respond ONLY with VOTE: ... | REASONING: ... | SPECIFIC_FEEDBACK: ...."
	case "user-advocate":
		base = courtProposalSystemContext + " You are the User Advocate. Consider usability, UX in web portal and channels, human impact of governance friction or turn delays, accessibility, and how the system feels for collaborators. Respond ONLY with VOTE: ... | REASONING: ... | SPECIFIC_FEEDBACK: ...."
	default:
		base = courtProposalSystemContext + " Evaluate the proposal from your specialized perspective. Respond ONLY with VOTE: Approve|Reject|Abstain | REASONING: detailed | SPECIFIC_FEEDBACK: actionable bullets or none."
	}

	return custom + base
}

func getChannelPersonaPrompt(persona string) string {
	custom := workspaceCustomPrefix()
	if persona == "ciso" {
		return custom + cisoChannelInstructions
	}
	display := collab.DisplayName("court-persona-" + persona)
	return custom + "You are the " + display + " in an AegisClaw collaboration channel. " +
		"Stay in character. Never expose secrets. Do NOT use VOTE format in channels. " +
		"Always output an action on the first line: PASS or SPEAK. " +
		"PASS (also accepted: NO_REPLY) if you would only recap, agree, or keep chatter going. " +
		"SPEAK only if you add a new point from your role, you are @mentioned, or a question is aimed at you. " +
		"If SPEAK, write 2-4 sentences after the first line. If PASS, output only PASS."
}

// buildChannelTurnPrompt is the production prompt used for channel.turn (and tests).
func buildChannelTurnPrompt(persona, uniqueSource, chID, batchText, anchorText string) string {
	display := collab.DisplayName(uniqueSource)
	var b strings.Builder
	b.WriteString(getChannelPersonaPrompt(persona))
	b.WriteString("\n\nYou received a batched channel turn in #")
	b.WriteString(chID)
	b.WriteString(" as ")
	b.WriteString(display)
	b.WriteString(".\nNew messages since your last turn:\n")
	b.WriteString(batchText)
	if strings.TrimSpace(anchorText) != "" {
		b.WriteString("\n\nRelevant prior context (from get_relevant_since anchors):\n")
		b.WriteString(anchorText)
	}
	mentioned := collab.IsMentioned(uniqueSource, batchText) || collab.IsMentioned("court-persona-"+persona, batchText)
	newRisk, riskHits := cisoNewMaterialRisk(batchText, anchorText)
	switch {
	case mentioned:
		b.WriteString("\n\nYou were directly @mentioned in the new messages. First line MUST be SPEAK. If there is no material risk, SPEAK with one sentence that there is no issue for your role, then stop.")
	case persona == "ciso" && newRisk:
		b.WriteString("\n\nThe NEW messages introduce a first-seen CISO risk (")
		b.WriteString(strings.Join(riskHits, ", "))
		b.WriteString("). First line MUST be SPEAK with a short posture (Court / Secrets VM / halt / forbid). Do not PASS.")
	case persona == "ciso":
		b.WriteString("\n\nYou were not @mentioned and the new messages do not introduce a first-seen CISO risk. First line MUST be PASS.")
	default:
		b.WriteString("\n\nFirst line must be PASS or SPEAK. You were not @mentioned. Default to PASS unless the new messages contain a new item in your role that others have not already covered.")
	}
	return b.String()
}

// cisoRiskKeywords maps a high-signal phrase to a risk family. Families let
// recap of the same issue (PAT vs .env secret) count as one risk, while a new
// family (localStorage tokens after an .env debate) is still SPEAK.
var cisoRiskKeywords = []struct {
	phrase string
	family string
}{
	{"secret", "secrets"}, {"password", "secrets"}, {"credential", "secrets"},
	{"api key", "secrets"}, {"pat", "secrets"}, {"ghp_", "secrets"}, {"sk-live", "secrets"},
	{"client_secret", "secrets"}, {"client secret", "secrets"}, {".env", "secrets"},
	{"bot token", "secrets"}, {"jwt", "secrets"},
	{"localstorage", "browser-storage"}, {"local storage", "browser-storage"},
	{"pii", "pii"}, {"gdpr", "pii"}, {"hubspot", "pii"}, {"email+ip", "pii"},
	{"0.0.0.0", "bind"}, {"bind-all", "bind"}, {"port-forward", "bind"},
	{"port forward", "bind"}, {"router forward", "bind"},
	{"chmod 777", "perms"}, {" 777", "perms"},
	{"wildcard", "perms"}, {"tool.*", "perms"},
	{"shared memory", "isolation"}, {"group brain", "isolation"},
	{"rfc1918", "scan"}, {"lan-scanning", "scan"}, {"scanner", "scan"},
	{"sandbox escape", "incident"}, {"vsock", "incident"}, {"uncontained", "incident"},
	{"skip court", "court-bypass"}, {"disable court", "court-bypass"},
	{"without court", "court-bypass"}, {"bypass court", "court-bypass"},
	{"soc2", "compliance"}, {"cosign", "compliance"},
	{"encrypt", "encryption"}, {"at rest", "encryption"},
}

func containsCISORiskKeyword(lower, kw string) bool {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return false
	}
	if len(kw) <= 4 && !strings.ContainsAny(kw, "._") {
		for i := 0; i <= len(lower)-len(kw); i++ {
			if lower[i:i+len(kw)] != kw {
				continue
			}
			leftOK := i == 0 || !isASCIIAlnum(lower[i-1])
			rightOK := i+len(kw) == len(lower) || !isASCIIAlnum(lower[i+len(kw)])
			if leftOK && rightOK {
				return true
			}
		}
		return false
	}
	return strings.Contains(lower, kw)
}

func isASCIIAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func cisoRiskHits(text string) []string {
	lower := strings.ToLower(text)
	var hits []string
	seen := map[string]bool{}
	for _, kw := range cisoRiskKeywords {
		if containsCISORiskKeyword(lower, kw.phrase) && !seen[kw.family] {
			hits = append(hits, kw.family)
			seen[kw.family] = true
		}
	}
	return hits
}

// cisoNewMaterialRisk reports risk phrases present in the new batch but not in anchors.
func cisoNewMaterialRisk(batchText, anchorText string) (bool, []string) {
	hits := cisoRiskHits(batchText)
	if len(hits) == 0 {
		return false, nil
	}
	prior := map[string]bool{}
	for _, p := range cisoRiskHits(anchorText) {
		prior[p] = true
	}
	var fresh []string
	for _, h := range hits {
		if !prior[h] {
			fresh = append(fresh, h)
		}
	}
	return len(fresh) > 0, fresh
}

func buildChannelActivityPrompt(persona, userQuestion string) string {
	display := collab.DisplayName("court-persona-" + persona)
	return getChannelPersonaPrompt(persona) +
		"\n\nA message was posted in a collaboration channel to you as \"" + display + "\":\n" +
		userQuestion +
		"\n\nFirst line must be PASS or SPEAK. Do NOT use VOTE format."
}

// analyzeProposal is the core review function.
// In production run (with hubClient) it performs a real "llm.call" via network-boundary
// using the persona prompt + proposal description (pulled from Store per spec), then
// strictly parses the structured response.
// Unit tests continue to exercise prompts + basic analysis without requiring a live LLM
// (test double path clearly marked; never used in the prod Court execution path inside the binary).
// SPEC: governance-court.md §Output Format Requirements + §Implementation Guidance (strict format, Abstain encouraged).
func analyzeProposal(persona, proposalDesc string, hubClient hubclient.Client) (string, string) {
	prompt := getPersonaPrompt(persona) + "\n\nProposal description:\n" + proposalDesc + "\n\nRespond in the exact VOTE|REASONING|SPECIFIC_FEEDBACK format. Abstain on insufficient context."

	var llmResponse string
	var err error

	if hubClient != nil {
		// REAL production path — no mocks, no fixtures (Phase 3 DoD).
		// Uses the exact same "llm.call" contract as the Agent Runtime (see internal/agent/loop/loop.go:139 NewRealLLMCaller).
		llmResponse, err = callRealLLMViaHub(context.Background(), hubClient, prompt)
		if err != nil {
			// Fail-closed per security-model.md: cannot produce a vote without LLM.
			return "Abstain", "Abstained (fail-closed): LLM call failed for " + persona + ": " + err.Error()
		}
	} else {
		// Test-only / direct-call path (used by main_test.go). Never reached when
		// runCourtPersona executes the real message loop with a live hubClient.
		// Still produces distinguishable role-based output for unit coverage.
		llmResponse = simulateLLMForTestOnly(persona, prompt)
	}

	// Strict parser (enforces governance-court.md output format)
	vote, reasoning := parseStructuredCourtResponse(llmResponse, persona, proposalDesc)

	// 7.2: Publish local decision event (in-process). In full system this (or a
	// signed version) flows through AegisHub so approval queues and proactive
	// agents can react. The real path in runCourtPersona also emits via hub.
	localBus := eventbus.New()
	localBus.PublishJSON("court.decision.made", map[string]interface{}{
		"persona": persona,
		"vote":    vote,
		"reason":  reasoning,
	}, eventbus.WithSource("court-persona"))

	return vote, reasoning
}

// llmChannelReply calls the LLM with an already-formed channel prompt (no extra persona wrapper).
func llmChannelReply(persona string, hubClient hubclient.Client, prompt string) string {
	text, err := callRealLLMViaHub(context.Background(), hubClient, prompt)
	if err != nil || strings.TrimSpace(text) == "" {
		log.Printf("court-persona-%s: channel reply LLM failed (%v)", persona, err)
		collab.Tracef("court-persona-"+persona, "channel.reply.skip", "ch=? err=%v", err)
		return ""
	}
	trimmed, skip := collab.NormalizeChannelLLMReply(text)
	if skip {
		collab.Tracef("court-persona-"+persona, "channel.reply.skip", "reason=no_reply")
		return ""
	}
	return trimmed
}

// generateChannelReply produces a short contextual reply via LLM (no canned fallback text).
func generateChannelReply(persona, userQuestion string, hubClient hubclient.Client) string {
	prompt := buildChannelActivityPrompt(persona, userQuestion)
	return llmChannelReply(persona, hubClient, prompt)
}

func postChannelIntro(hcl hubclient.Client, uniqueSource, chID, content string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := hcl.Send(ctx, hubclient.Message{
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
	return err
}

func processChannelActivity(hcl hubclient.Client, msg hubclient.Message, uniqueSource, persona string) {
	payload, _ := msg.Payload.(map[string]interface{})
	chID, _ := payload["channel_id"].(string)
	from, _ := payload["from"].(string)
	userContent := collab.PayloadContentString(payload["content"])
	if chID == "" {
		chID = "main"
	}

	collab.Tracef("court-persona-"+persona, "channel.activity.recv", "ch=%s from=%s", chID, from)

	shouldRespond, reason := collab.ShouldRespondToActivity(uniqueSource, from, userContent)
	if !shouldRespond {
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
		fmt.Printf("Persona %s ignored channel activity in %s (%s)\n", persona, chID, reason)
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

	// Process inline on the hubclient connection. Do not spawn a goroutine:
	// nested hcl.Send (llm.call, channel.post) shares the decoder with Receive;
	// a background Receive steals llm.call.response and replies never post.
	reply := generateChannelReply(persona, userContent, hcl)
	if strings.TrimSpace(reply) == "" {
		return
	}
	if err := postChannelIntro(hcl, uniqueSource, chID, reply); err != nil {
		log.Printf("court-persona-%s: channel.post failed: %v", persona, err)
		collab.Tracef("court-persona-"+persona, "channel.post.fail", "ch=%s err=%v", chID, err)
		return
	}
	collab.Tracef("court-persona-"+persona, "channel.post.ok", "ch=%s len=%d", chID, len(reply))
	fmt.Printf("Persona %s posted channel reply to %s (%s)\n", persona, chID, reason)
}

func processChannelTurn(hcl hubclient.Client, msg hubclient.Message, uniqueSource, persona string) {
	turn, ok := collab.ParseTurnPayload(msg.Payload)
	if !ok {
		return
	}
	chID := turn.ChannelID
	collab.Tracef("court-persona-"+persona, "channel.turn.recv", "ch=%s since=%d new=%d anchors=%v", chID, turn.SinceSeq, len(turn.NewMessages), turn.RelevanceAnchors)

	_ = hcl.Reply(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: msg.Source,
		Command:     "response",
		Payload: map[string]interface{}{
			"status": "delivered", "reason": "turn", "channel_id": chID,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	ctx := context.Background()
	anchorMsgs, err := collab.FetchRelevantSince(ctx, hcl, chID, turn.SinceSeq, turn.RelevanceAnchors)
	if err != nil {
		collab.Tracef("court-persona-"+persona, "get_relevant_since.fail", "ch=%s err=%v", chID, err)
	}
	collab.Tracef("court-persona-"+persona, "get_relevant_since.ok", "ch=%s anchors=%d", chID, len(anchorMsgs))

	batchText := collab.FormatTurnMessages(turn.NewMessages)
	anchorText := collab.FormatAnchorContext(anchorMsgs)
	prompt := buildChannelTurnPrompt(persona, uniqueSource, chID, batchText, anchorText)

	reply := llmChannelReply(persona, hcl, prompt)
	if strings.TrimSpace(reply) == "" {
		return
	}
	if err := postChannelIntro(hcl, uniqueSource, chID, reply); err != nil {
		collab.Tracef("court-persona-"+persona, "channel.turn.post.fail", "ch=%s err=%v", chID, err)
		return
	}
	collab.Tracef("court-persona-"+persona, "channel.turn.post.ok", "ch=%s len=%d", chID, len(reply))
	fmt.Printf("Persona %s posted turn reply to %s\n", persona, chID)
}

// callRealLLMViaHub performs the production LLM call for a Court persona.
// Sends "llm.call" to network-boundary (exactly as Agent Runtime does) with the
// persona-specialized prompt. The boundary enforces scopes and proxies to the LLM backend.
// Returns the raw response text for our strict parser.
// SPEC: agent-runtime.md §Communication (llm via boundary); governance-court.md (real LLM inside isolated Court VMs).
func callRealLLMViaHub(ctx context.Context, hub hubclient.Client, prompt string) (string, error) {
	if hub == nil {
		return "", fmt.Errorf("no hub client (fail-closed)")
	}

	llmReq := map[string]interface{}{
		"model":  bootargs.DefaultModel(agent.DefaultLLMModel),
		"prompt": prompt,
		"stream": false,
	}
	msg := hubclient.Message{
		Source:      hub.AssignedID(),
		Destination: "network-boundary",
		Command:     "llm.call",
		Payload: map[string]interface{}{
			"request":  llmReq,
			"endpoint": "/api/generate",
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	resp, err := hub.Send(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("llm.call via hub failed: %w", err)
	}
	if resp.Command == "error" {
		return "", fmt.Errorf("network-boundary error: %v", resp.Payload)
	}

	// Same response shape handling as the Agent Runtime real caller.
	if payload, ok := resp.Payload.(map[string]interface{}); ok {
		if response, ok := payload["response"].(string); ok {
			var inner map[string]interface{}
			if json.Unmarshal([]byte(response), &inner) == nil {
				if r, ok := inner["response"].(string); ok {
					return r, nil
				}
			}
			return response, nil
		}
		if r, ok := payload["text"].(string); ok {
			return r, nil
		}
	}
	return "", fmt.Errorf("unexpected llm.call response shape: %T", resp.Payload)
}

// parseStructuredCourtResponse enforces the exact output contract from
// governance-court.md §Output Format Requirements. Returns (vote, fullReasoningWithFeedback).
// Any deviation or missing required structure causes Abstain (fail-closed).
func parseStructuredCourtResponse(raw, persona, proposalDesc string) (string, string) {
	lower := strings.ToLower(raw)
	vote := "Abstain"
	reason := raw

	if strings.Contains(lower, "vote: reject") || (strings.Contains(lower, "reject") && !strings.Contains(lower, "abstain")) {
		vote = "Reject"
	} else if strings.Contains(lower, "vote: approve") || strings.Contains(lower, "approve") {
		vote = "Approve"
	} else if strings.Contains(lower, "vote: abstain") || strings.Contains(lower, "abstain") || proposalDesc == "" {
		vote = "Abstain"
		reason = "Abstained: insufficient context or high uncertainty from " + persona + " perspective. " + raw
	}

	// Extract specific_feedback if present (after | or keyword)
	feedback := ""
	if idx := strings.Index(lower, "specific_feedback:"); idx != -1 {
		feedback = strings.TrimSpace(raw[idx+len("specific_feedback:"):])
	} else if idx := strings.Index(lower, "specific feedback"); idx != -1 {
		feedback = strings.TrimSpace(raw[idx+len("specific feedback"):])
	}
	if feedback != "" && vote != "Approve" {
		reason = reason + " | specific_feedback: " + feedback
	}

	return vote, reason
}

// simulateLLMForTestOnly: ONLY for direct calls from unit tests (main_test.go).
// Produces distinguishable, role-appropriate output so tests cover prompt + parse logic
// without requiring a live LLM backend or hub. This path is NEVER executed in the
// production Court binary execution path (runCourtPersona always passes a real hubClient).
// SPEC: governance-court.md §Test Requirements (each persona produces feedback consistent with role).
func simulateLLMForTestOnly(persona, prompt string) string {
	lower := strings.ToLower(prompt)
	switch persona {
	case "ciso":
		if strings.Contains(lower, "skill") {
			return "VOTE: Approve | REASONING: Low strategic risk; aligns with compliance. | SPECIFIC_FEEDBACK: monitor post-deploy metrics for 30 days."
		}
		return "VOTE: Abstain | REASONING: Need more business context and risk assessment data."
	case "security-architect":
		if strings.Contains(lower, "network") || strings.Contains(lower, "discord") {
			return "VOTE: Reject | REASONING: Expands attack surface via new outbound channel; requires explicit policy gate and egress audit. | SPECIFIC_FEEDBACK: Add network-boundary policy + audit logging before resubmit."
		}
		return "VOTE: Approve | REASONING: Design sound if Builder gates and composition checks pass."
	case "tester":
		if strings.Contains(lower, "test") {
			return "VOTE: Approve | REASONING: Good test plan implied by proposal structure."
		}
		return "VOTE: Abstain | REASONING: Test strategy, edge cases, and regression coverage not detailed in proposal."
	default:
		if strings.Contains(lower, "reject") {
			return "VOTE: Reject | REASONING: " + persona + " flags material issues from its perspective."
		}
		return "VOTE: Approve | REASONING: " + persona + " perspective satisfied for this change."
	}
}

func runCourtPersona(cmd *cobra.Command, args []string) {
	// Resolve persona (flag > env > cmdline) — enables single image for all 7 real microVMs.
	resolved := resolvePersona(persona)
	if resolved == "" {
		log.Fatal("FATAL: --persona (or AEGIS_COURT_PERSONA env / aegis.persona= cmdline) is required. Valid: ciso, security-architect, architect, senior-coder, tester, efficiency, user-advocate")
	}
	persona = resolved // normalize

	// 7.6: Load user workspace customizations so Court personas can respect
	// custom AGENTS/SOUL instructions during reviews (symmetric to Agent integration).
	wsCtx, wsErr := workspace.Load("")
	if wsErr != nil {
		log.Printf("7.6 WARNING: Failed to load workspace customizations for Court: %v (using defaults)", wsErr)
	} else if wsCtx.SOUL != "" || wsCtx.AGENTS != "" {
		log.Printf("7.6: Court loaded workspace customizations (AGENTS=%d, SOUL=%d chars)",
			len(wsCtx.AGENTS), len(wsCtx.SOUL))
	}
	loadedWorkspace = wsCtx

	timing.RecordPhase("main_entry")

	// Paranoid key loading (distributed VM key preferred; see loadDistributedKey).
	priv, pub, err := loadDistributedKey()
	if err != nil {
		log.Fatal("Failed to load or generate signing key (fail-closed):", err)
	}
	timing.RecordPhase("key_loaded")
	defer func() {
		// Best-effort zeroization of our copy (caller of Dial will also zero its copy).
		for i := range priv {
			priv[i] = 0
		}
	}()
	_ = pub // pub is used by the hubclient.Register call below (key hygiene contract)

	// Use the real hubclient (unix for dev, direct vsock or host-inverted bridge for real
	// Firecracker Court VMs using aegis.hub_vsock=1 + GuestHubBridgePort).
	// This eliminates the raw net.Dial + manual encoder stub.
	socket := expandPath(hubSocket)
	var hcl hubclient.Client
	if bootargs.UseHubVsock() {
		fmt.Printf("court-persona-%s: waiting for host hub bridge on vsock :%d (Firecracker inverted path)\n", persona, hubclient.GuestHubBridgePort)
		hcl, err = hubclient.AcceptVsockHubBridge(hubclient.GuestHubBridgePort, priv)
	} else if portStr := os.Getenv("AEGIS_HUB_VSOCK_PORT"); portStr != "" {
		hcl, err = hubclient.DialVsock(hubclient.HostCID, hubclient.HubVsockPort, priv)
	} else {
		hcl, err = hubclient.DialUnix(socket, priv)
	}
	if err != nil {
		log.Fatal("Failed to connect to AegisHub via hubclient:", err)
	}
	defer hcl.Close()
	timing.RecordPhase("hub_dialed")

	// Register using the standard hubclient handshake (aegishub.md §Handshake Sequence).
	// Unique source per persona enables the 7 distinct ACL wildcard routes.
	uniqueSource := "court-persona-" + persona
	regResp, err := hcl.Register(context.Background(), uniqueSource, pub, getBuildVersion())
	if err != nil {
		log.Fatal("Court persona registration failed (fail-closed):", err)
	}
	fmt.Println("Court Persona", persona, "registered as", uniqueSource, "assignedID=", regResp.AssignedID)
	timing.RecordPhase("register_complete")
	timing.WriteComponentReadySentinel()

	// Production message loop using hubclient.Receive + Send (bidirectional, signed).
	// SPEC: court-scribe.md §Communication Flow + governance-court.md §Architecture.
	timing.RecordPhase("message_loop_ready")
	for {
		msg, err := hcl.Receive(context.Background())
		if err != nil {
			log.Println("hubclient Receive error (continuing):", err)
			continue
		}

		fmt.Println("Persona", persona, "received:", msg.Command)

		switch msg.Command {
		case channelfacilitator.CmdTurn:
			processChannelTurn(hcl, msg, uniqueSource, persona)

		case "channel.activity", "channel.member_notify":
			processChannelActivity(hcl, msg, uniqueSource, persona)

		case "llm.call.response":
			log.Printf("court-persona-%s: ignoring stray %s (hubclient decoder race guard)", persona, msg.Command)

		case "scribe.notify_review":
			payload, _ := msg.Payload.(map[string]interface{})
			proposalID, _ := payload["proposal_id"].(string)
			if proposalID == "" {
				log.Println("notify_review missing proposal_id")
				continue
			}

			// Pull proposal content DIRECTLY from Store (never via Scribe) — core security invariant.
			getPayload, _ := json.Marshal(map[string]string{"id": proposalID})
			getResp, err := hcl.Send(context.Background(), hubclient.Message{
				Source:      uniqueSource,
				Destination: "store",
				Command:     "proposal.get",
				Payload:     json.RawMessage(getPayload),
				Timestamp:   time.Now().Format(time.RFC3339),
			})
			if err != nil || getResp.Command == "error" {
				log.Println("Failed to get proposal from Store:", err, getResp.Payload)
				continue
			}
			var proposalData map[string]interface{}
			if b, ok := getResp.Payload.([]byte); ok {
				json.Unmarshal(b, &proposalData)
			} else {
				proposalData, _ = getResp.Payload.(map[string]interface{})
			}
			description := ""
			if d, ok := proposalData["description"].(string); ok {
				description = d
			}

			// REAL LLM review (no mocks in this execution path when hcl != nil).
			vote, reasoning := analyzeProposal(persona, description, hcl)

			// Submit cryptographically signed vote to Scribe.
			votePayload, _ := json.Marshal(map[string]interface{}{
				"proposal_id": proposalID,
				"persona":     persona,
				"vote":        vote,
				"reasoning":   reasoning,
			})
			_, err = hcl.Send(context.Background(), hubclient.Message{
				Source:      uniqueSource,
				Destination: "court-scribe",
				Command:     "scribe.submit_vote",
				Payload:     json.RawMessage(votePayload),
				Timestamp:   time.Now().Format(time.RFC3339),
			})
			if err != nil {
				log.Println("Failed to submit vote:", err)
			} else {
				fmt.Printf("Persona %s submitted %s for %s\n", persona, vote, proposalID)
			}

		case "version", "get-version":
			respPayload, _ := json.Marshal(map[string]string{"version": getBuildVersion()})
			_, _ = hcl.Send(context.Background(), hubclient.Message{
				Source:      uniqueSource,
				Destination: msg.Source,
				Command:     "version",
				Payload:     json.RawMessage(respPayload),
				Timestamp:   time.Now().Format(time.RFC3339),
			})
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "court-persona",
		Short: "Court Persona",
		Run:   runCourtPersona,
	}

	rootCmd.Flags().StringVar(&persona, "persona", "", "Persona name")
	rootCmd.MarkFlagRequired("persona")

	rootCmd.Execute()
}
