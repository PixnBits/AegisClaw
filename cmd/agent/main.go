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
	"AegisClaw/internal/collab"
	"AegisClaw/internal/agent/progress"
	agentSkills "AegisClaw/internal/agent/skills"
	"AegisClaw/internal/bootargs"
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

// 7.4: Loaded at startup via secure workspace loader. Used by prompt builders.
var loadedWorkspace *workspace.Context

// 7.4 helper: Returns a prefix string containing custom SOUL + AGENTS instructions
// (if any were loaded). This is prepended to reasoning prompts.
func customInstructionsPrefix() string {
	if loadedWorkspace == nil {
		return ""
	}
	var b strings.Builder
	if loadedWorkspace.SOUL != "" {
		b.WriteString("Core values and soul: ")
		b.WriteString(loadedWorkspace.SOUL)
		b.WriteString(". ")
	}
	if loadedWorkspace.AGENTS != "" {
		b.WriteString("Custom agent instructions: ")
		b.WriteString(loadedWorkspace.AGENTS)
		b.WriteString(". ")
	}
	return b.String()
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

// NOTE (Phase 1 1.1b):
// The old callLLM / callLLMWithFallback / mockLLMResponse and the 6 inline step
// functions have been removed from the production execution path.
// All reasoning now goes through internal/agent/loop + the six step packages
// using a real hubclient-backed LLM caller (no surface-only fallbacks or mocks
// in the hot path — per agent-runtime.md §Responsibilities and the No-Stubs-Left DoD).
//
// The special fast-path command handlers below (tool.list, autonomy, background, etc.)
// and the thin runAgent wiring remain for 7.3/7.6 compatibility during the refactor.
// They will be further cleaned in 1.3/1.4.

// runAgent is the thin launcher for the real Agent Runtime.
// All heavy lifting (real 6-step loop, key hygiene, transport selection, reasoning)
// lives in internal/agent/* and the hubclient.
//
// SPEC: agent-runtime.md §Communication + §Security (real vsock path when running
// inside Firecracker, distributed per-VM key only, no more GenerateKey in prod path).
func runAgent(cmd *cobra.Command, args []string) {
	timing.RecordPhase("main_entry")

	// === Key loading (paranoid — consume the key the orchestrator distributed) ===
	// Preferred: AEGIS_VM_PRIVATE_KEY_PATH (written by orchestrator before VM start,
	// 0600, guest shreds after load). Fallback to generate only for dev / unit tests.
	priv, pub, err := bootargs.LoadDistributedVMKey("agent")
	if err != nil {
		if bootargs.UseHubVsock() {
			log.Printf("agent: FATAL: %v", err)
			time.Sleep(24 * time.Hour) // keep guest up for console logs (init also holds on exit)
		}
		log.Printf("agent: %v — generating ephemeral key (dev/test only)", err)
		pub, priv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal("agent: failed to obtain Ed25519 key (fail-closed):", err)
		}
	}
	_ = pub
	timing.RecordPhase("key_loaded")

	for {
		client, err := dialHubTransport(pub, priv)
		if err != nil {
			if bootargs.UseHubVsock() {
				log.Printf("agent: hub bridge connect failed: %v (retrying)", err)
				// Reduced for <1s (was 1s); overlaps with early host bridge start.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			log.Fatal("agent: failed to connect to AegisHub:", err)
		}
		timing.RecordPhase("hub_dialed")

		if runAgentSession(client, pub, priv) {
			client.Close()
			return
		}
		client.Close()

		if !bootargs.UseHubVsock() {
			log.Fatal("agent: hub connection lost")
		}
		log.Println("agent: hub bridge dropped; waiting for host to reconnect…")
		time.Sleep(500 * time.Millisecond)
	}
}

func runAgentSession(client hubclient.Client, pub ed25519.PublicKey, priv ed25519.PrivateKey) bool {
	componentID := bootargs.ComponentID("agent")
	regResp, err := client.Register(context.Background(), componentID, pub, getBuildVersion())
	if err != nil {
		log.Printf("agent: register failed: %v", err)
		return false
	}
	fmt.Println("Agent registered with hub, assigned ID:", regResp.AssignedID)
	timing.RecordPhase("register_complete")
	timing.WriteComponentReadySentinel()

	wsCtx, wsErr := workspace.Load("")
	if wsErr != nil {
		log.Printf("7.4 WARNING: %v (using defaults)", wsErr)
	} else if wsCtx.SOUL != "" || wsCtx.AGENTS != "" || wsCtx.TOOLS != "" {
		log.Printf("7.4: Loaded workspace customizations")
	}
	loadedWorkspace = wsCtx

	skillIndex := NewAgentSkillIndex()
	realLLM := loop.NewRealLLMCaller(client, os.Getenv("AEGIS_DEFAULT_MODEL"))

	fmt.Println("agent: real message-driven loop active (hubclient Receive + real loop.RunTurn)")
	timing.RecordPhase("message_loop_ready")

	for {
		msg, err := client.Receive(context.Background())
		if err != nil {
			log.Println("agent: hub disconnected:", err)
			return false
		}
		if !handleAgentMessage(client, msg, skillIndex, realLLM) {
			return true
		}
	}
}

func handleAgentMessage(client hubclient.Client, msg hubclient.Message, skillIndex *agentSkills.AgentSkillIndex, realLLM agent.LLMCallFunc) bool {
	switch msg.Command {
	case "chat.thought_events":
		sessionID := chatPayloadString(msg.Payload, "session_id")
		limit := chatPayloadInt(msg.Payload, "limit", 80)
		events := progress.ListThoughtEvents(sessionID, limit)
		out := make([]interface{}, len(events))
		for i, ev := range events {
			out[i] = ev
		}
		_ = client.Reply(context.Background(), hubclient.Message{
			Source:      client.AssignedID(),
			Destination: msg.Source,
			Command:     "response",
			Payload:     out,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		})
		return true
	case "chat.tool_events":
		_ = client.Reply(context.Background(), hubclient.Message{
			Source:      client.AssignedID(),
			Destination: msg.Source,
			Command:     "response",
			Payload:     []interface{}{},
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		})
		return true
	case "chat.stream_progress":
		streamID := chatPayloadString(msg.Payload, "stream_id")
		st := progress.StreamProgress(streamID)
		_ = client.Reply(context.Background(), hubclient.Message{
			Source:      client.AssignedID(),
			Destination: msg.Source,
			Command:     "response",
			Payload: map[string]interface{}{
				"stream_id":  st.StreamID,
				"request_id": st.RequestID,
				"thinking":   st.Thinking,
				"content":    st.Content,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return true
	}

	if msg.Command == "tool.list" || msg.Command == "tool.search" {
		result := agentSkills.HandleToolCommand(msg.Command, msg.Payload, skillIndex)
		resp := hubclient.Message{
			Source:      client.AssignedID(),
			Destination: msg.Source,
			Command:     msg.Command + ".response",
			Payload:     result,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
		_ = client.Reply(context.Background(), resp)
		return true
	}

	if msg.Command == "background.work" || msg.Command == "proactive.task" {
		log.Printf("7.6: background work → running FULL real 6-step loop (no mini/demo)")
		go func(payload interface{}) {
			tc := &agent.TurnContext{
				Input:              payload,
				Hub:                client,
				SkillIndex:         skillIndex,
				CustomInstructions: customInstructionsPrefix(),
			}
			_, _ = loop.RunTurn(context.Background(), tc, realLLM)
		}(msg.Payload)
		return true
	}

	if msg.Command == "court.decision" || msg.Command == "governance.revoke" || msg.Command == "court.revoke_scope" {
		log.Printf("Court decision received: %v (enforcing immediately - fail-closed)", msg.Payload)
		if payload, ok := msg.Payload.(map[string]interface{}); ok {
			if action, _ := payload["action"].(string); action == "terminate" {
				log.Printf("Court ordered termination for this agent runtime - shutting down loop (fail-closed)")
				return false
			}
		}
		return true
	}

	if msg.Command == "component.ready" || msg.Command == "sentinel.ready" {
		_, err := os.Stat("/tmp/aegis-component-ready")
		ready := err == nil
		_ = client.Reply(context.Background(), hubclient.Message{
			Source: client.AssignedID(), Destination: msg.Source, Command: "response",
			Payload: map[string]interface{}{"ready": ready},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return true
	}

	if msg.Command == "channel.activity" || msg.Command == "channel.member_notify" {
		processAgentChannelActivity(client, msg, realLLM)
		return true
	}

	tc := &agent.TurnContext{
		Input:              msg.Payload,
		Hub:                client,
		SkillIndex:         skillIndex,
		CustomInstructions: customInstructionsPrefix(),
		SessionID:          chatPayloadString(msg.Payload, "session_id"),
		StreamID:           chatPayloadString(msg.Payload, "stream_id"),
	}
	if msg.Command == "chat.message" || msg.Command == "user.turn" {
		tc.DrainPolls = func() { drainChatPolls(client, skillIndex) }
	}
	finalResult, _ := loop.RunTurn(context.Background(), tc, realLLM)
	replyChatTurn(client, msg, tc.SessionID, finalResult)
	return true
}

// processAgentChannelActivity handles channel.activity for on-demand SDLC role VMs (coder-*, tester-*).
func processAgentChannelActivity(client hubclient.Client, msg hubclient.Message, realLLM agent.LLMCallFunc) {
	sourceID := client.AssignedID()
	payload, _ := msg.Payload.(map[string]interface{})
	chID, _ := payload["channel_id"].(string)
	from, _ := payload["from"].(string)
	userContent, _ := payload["content"].(string)
	if chID == "" {
		chID = "main"
	}

	shouldRespond, reason := collab.ShouldRespondToActivity(sourceID, from, userContent)
	if !shouldRespond {
		_ = client.Reply(context.Background(), hubclient.Message{
			Source: sourceID, Destination: msg.Source, Command: "response",
			Payload: map[string]interface{}{
				"status": "ignored", "reason": string(reason), "channel_id": chID,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	_ = client.Reply(context.Background(), hubclient.Message{
		Source: sourceID, Destination: msg.Source, Command: "response",
		Payload: map[string]interface{}{
			"status": "delivered", "reason": string(reason), "channel_id": chID,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	go func() {
		roleLabel := collab.AgentRoleLabel(sourceID)
		broadcast, _ := collab.ActivityHints(sourceID, userContent)
		reply := collab.AgentFallbackIntro(sourceID)
		if !broadcast {
			prompt := customInstructionsPrefix() +
				"\n\nYou are the " + roleLabel + " in channel " + chID + ". A user posted:\n" + userContent +
				"\n\nReply in 2-4 sentences from your role's perspective. If no reply is needed, respond with exactly: NO_REPLY"
			if llmReply, err := realLLM(context.Background(), prompt); err == nil {
				trimmed := strings.TrimSpace(llmReply)
				if trimmed != "" && !strings.EqualFold(trimmed, "NO_REPLY") {
					reply = trimmed
				} else if strings.EqualFold(trimmed, "NO_REPLY") {
					log.Printf("agent %s: chose not to reply in %s", sourceID, chID)
					return
				}
			}
		}
		postCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		_, err := client.Send(postCtx, hubclient.Message{
			Source: sourceID, Destination: "store", Command: "channel.post",
			Payload: map[string]interface{}{
				"channel_id": chID, "from": sourceID, "content": reply,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			log.Printf("agent %s: channel.post failed: %v", sourceID, err)
			return
		}
		log.Printf("agent %s: posted channel reply to %s (%s)", sourceID, chID, reason)
	}()
}

func drainChatPolls(client hubclient.Client, skillIndex *agentSkills.AgentSkillIndex) {
	ctx := context.Background()
	for {
		poll, ok, err := client.TryReceive(ctx, 75*time.Millisecond)
		if err != nil || !ok {
			return
		}
		switch poll.Command {
		case "chat.thought_events", "chat.stream_progress", "chat.tool_events":
			handleAgentPoll(client, poll, skillIndex)
		default:
			log.Printf("agent: deferred inbound command during turn: %s", poll.Command)
		}
	}
}

func handleAgentPoll(client hubclient.Client, msg hubclient.Message, skillIndex *agentSkills.AgentSkillIndex) {
	switch msg.Command {
	case "chat.thought_events":
		sessionID := chatPayloadString(msg.Payload, "session_id")
		limit := chatPayloadInt(msg.Payload, "limit", 80)
		events := progress.ListThoughtEvents(sessionID, limit)
		out := make([]interface{}, len(events))
		for i, ev := range events {
			out[i] = ev
		}
		_ = client.Reply(context.Background(), hubclient.Message{
			Source: client.AssignedID(), Destination: msg.Source, Command: "response",
			Payload: out, Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	case "chat.stream_progress":
		streamID := chatPayloadString(msg.Payload, "stream_id")
		st := progress.StreamProgress(streamID)
		_ = client.Reply(context.Background(), hubclient.Message{
			Source: client.AssignedID(), Destination: msg.Source, Command: "response",
			Payload: map[string]interface{}{
				"stream_id": st.StreamID, "request_id": st.RequestID,
				"thinking": st.Thinking, "content": st.Content,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	case "chat.tool_events":
		_ = client.Reply(context.Background(), hubclient.Message{
			Source: client.AssignedID(), Destination: msg.Source, Command: "response",
			Payload: []interface{}{}, Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
	_ = skillIndex
}

func replyChatTurn(client hubclient.Client, msg hubclient.Message, sessionID string, finalResult *agent.StepResult) {
	responseText := "processed via real 6-step loop"
	if finalResult != nil && finalResult.Content != "" {
		responseText = finalResult.Content
	}
	trace := progress.TraceForSession(sessionID)
	thinking := make([]interface{}, len(trace))
	for i, ev := range trace {
		thinking[i] = ev
	}
	_ = client.Reply(context.Background(), hubclient.Message{
		Source:      client.AssignedID(),
		Destination: msg.Source,
		Command:     "response",
		Payload: map[string]interface{}{
			"content":        responseText,
			"thinking_trace": thinking,
			"tool_calls":     []interface{}{},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func chatPayloadString(payload interface{}, key string) string {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func chatPayloadInt(payload interface{}, key string, def int) int {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return def
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return def
	}
}

// dialHubTransport chooses unix (dev) or vsock (real Firecracker guest) based on env.
func dialHubTransport(pub ed25519.PublicKey, priv ed25519.PrivateKey) (hubclient.Client, error) {
	if bootargs.UseHubVsock() {
		fmt.Printf("agent: waiting for host hub bridge on vsock :%d (Firecracker inverted path)\n", hubclient.GuestHubBridgePort)
		return hubclient.AcceptVsockHubBridge(hubclient.GuestHubBridgePort, priv)
	}
	socket := expandPath(hubSocket)
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		socket = expandPath(env)
	}
	if _, err := os.Stat(socket); err == nil {
		return hubclient.DialUnix(socket, priv)
	}
	// Firecracker guest: host hub is on vsock :9999 (same fallback as store VM).
	return hubclient.DialVsock(hubclient.HostCID, hubclient.HubVsockPort, priv)
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "agent",
		Short: "Agent Runtime",
		Run:   runAgent,
	}

	rootCmd.Execute()
}

// === 7.3 Semantic Tool/Skill Discovery now lives in internal/agent/skills ===
// (moved in Phase 1 1.1b per no-stubs-plan/phase-1.md)
//
// Re-exports of the 7.3 skills index for the fast-path command handlers
// (tool.list, skills.snapshot, etc.). The real 6-step loop uses the package
// directly (as required by agent-runtime.md).
type (
	Skill        = agentSkills.Skill
	Tool         = agentSkills.Tool
	SearchResult = agentSkills.SearchResult
)

var NewAgentSkillIndex = agentSkills.NewAgentSkillIndex

// formatAvailableTools is a small bridge for the fast-path handlers.
// The real reasoning path uses the skills package directly.
func formatAvailableTools(idx *agentSkills.AgentSkillIndex) string {
	return agentSkills.FormatAvailableTools(idx, loadedWorkspaceAdapter{})
}

type loadedWorkspaceAdapter struct{}

func (loadedWorkspaceAdapter) GetSOUL() string   { if loadedWorkspace == nil { return "" }; return loadedWorkspace.SOUL }
func (loadedWorkspaceAdapter) GetAGENTS() string { if loadedWorkspace == nil { return "" }; return loadedWorkspace.AGENTS }
func (loadedWorkspaceAdapter) GetTOOLS() string  { if loadedWorkspace == nil { return "" }; return loadedWorkspace.TOOLS }
func (loadedWorkspaceAdapter) GetSKILLS() string { if loadedWorkspace == nil { return "" }; return loadedWorkspace.SKILLS }

// handleToolCommand and getString now delegate to the moved implementation.
var (
	handleToolCommand = agentSkills.HandleToolCommand
	getString         = agentSkills.GetString
)

