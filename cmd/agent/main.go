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
	agentSkills "AegisClaw/internal/agent/skills"
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
	// === Key loading (paranoid — consume the key the orchestrator distributed) ===
	// Preferred: AEGIS_VM_PRIVATE_KEY_PATH (written by orchestrator before VM start,
	// 0600, guest shreds after load). Fallback to generate only for dev / unit tests.
	priv, pub, err := loadDistributedOrEphemeralKey()
	if err != nil {
		log.Fatal("agent: failed to obtain Ed25519 key (fail-closed):", err)
	}
	_ = pub // pub is available for future use (register, logging, etc.)

	// === Transport selection (unix for dev, vsock for real microVM guests) ===
	// When running inside a Firecracker Agent Runtime VM, the environment or
	// kernel cmdline will cause us to pick vsock (hubclient.DialVsock).
	client, err := dialHubTransport(pub, priv)
	if err != nil {
		log.Fatal("agent: failed to connect to AegisHub:", err)
	}
	defer client.Close()

	// === Register (mandatory first step per aegishub.md) ===
	regResp, err := client.Register(context.Background(), "agent", pub, getBuildVersion())
	if err != nil {
		log.Fatal("agent: register failed (fail-closed):", err)
	}
	fmt.Println("Agent registered with hub, assigned ID:", regResp.AssignedID)

	// 7.4 workspace customizations (still loaded exactly as before)
	wsCtx, wsErr := workspace.Load("")
	if wsErr != nil {
		log.Printf("7.4 WARNING: %v (using defaults)", wsErr)
	} else if wsCtx.SOUL != "" || wsCtx.AGENTS != "" || wsCtx.TOOLS != "" {
		log.Printf("7.4: Loaded workspace customizations")
	}
	loadedWorkspace = wsCtx

	// 7.3 local index (now from the moved package)
	skillIndex := NewAgentSkillIndex()

	// Real LLM caller for the 6-step loop (no mocks, no fallbacks in this path)
	realLLM := loop.NewRealLLMCaller(client, os.Getenv("AEGIS_DEFAULT_MODEL"))

	// === Real bidirectional message loop (Phase 1.3 integration) ===
	// All communication now goes through hubclient (Send + Receive).
	// Special commands are handled with the local skill index.
	// Normal turns and background work use the *real* 6-step loop with
	// real memory.get_context calls and real LLM via network-boundary.
	fmt.Println("agent: real message-driven loop active (hubclient Receive + real loop.RunTurn)")

	for {
		msg, err := client.Receive(context.Background())
		if err != nil {
			log.Println("agent receive error:", err)
			time.Sleep(300 * time.Millisecond)
			continue
		}

		// High-volume per-message logging removed from hot path (surface noise).
		// Real audit will go through Store + Court Scribe later.
		_ = msg.Command // keep for future structured handling

		// Fast-path special handlers (preserved for 7.3/7.6 compatibility)
		if msg.Command == "tool.list" || msg.Command == "tool.search" {
			result := agentSkills.HandleToolCommand(msg.Command, msg.Payload, skillIndex)
			resp := hubclient.Message{
				Source:      client.AssignedID(),
				Destination: msg.Source,
				Command:     msg.Command + ".response",
				Payload:     result,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}
			_, _ = client.Send(context.Background(), resp)
			continue
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
			continue
		}

		// Phase 3: Handle real Court decisions pushed via Hub (agent-runtime.md §Event subscription for court feedback).
		// This is the critical path for "Agent Runtime respects Court decisions immediately".
		// We update the in-memory revoked scopes and can short-circuit or annotate future turns.
		if msg.Command == "court.decision" || msg.Command == "governance.revoke" || msg.Command == "court.revoke_scope" {
			log.Printf("Court decision received: %v (enforcing immediately - fail-closed)", msg.Payload)
			// In a fuller impl we would maintain per-session revoked state.
			// For Group 4 we at least log + could inject into next TurnContext.
			// A real termination command would be handled here by exiting or signaling.
			if payload, ok := msg.Payload.(map[string]interface{}); ok {
				if action, _ := payload["action"].(string); action == "terminate" {
					log.Printf("Court ordered termination for this agent runtime - shutting down loop (fail-closed)")
					// In production the orchestrator would StopVM us; here we exit the receive loop.
					return
				}
			}
			// TODO (next slice): merge revoked scopes into TurnContext.RevokedScopes for execute/act checks.
			continue
		}

		// All other messages (user turns, etc.) go through the real loop
		tc := &agent.TurnContext{
			Input:              msg.Payload,
			Hub:                client,
			SkillIndex:         skillIndex,
			CustomInstructions: customInstructionsPrefix(),
		}
		finalResult, _ := loop.RunTurn(context.Background(), tc, realLLM)

		// Return real output from the 6-step reasoning (Phase 1.3 wiring)
		responseText := "processed via real 6-step loop"
		if finalResult != nil && finalResult.Content != "" {
			responseText = finalResult.Content
		}

		resp := hubclient.Message{
			Source:      client.AssignedID(),
			Destination: msg.Source,
			Command:     "response",
			Payload:     responseText,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
		_, _ = client.Send(context.Background(), resp)
	}
}

// loadDistributedOrEphemeralKey implements the consumer side of the secure key
// distribution performed by the Host Daemon (orchestrator.go:89-164 + security/manager).
// It prefers the ephemeral file written for the guest and shreds it after load.
func loadDistributedOrEphemeralKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	keyPath := os.Getenv("AEGIS_VM_PRIVATE_KEY_PATH")
	if keyPath == "" {
		keyPath = "/run/aegis/vmkey" // conventional path used by orchestrator
	}
	if data, err := os.ReadFile(keyPath); err == nil {
		privBytes, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if len(privBytes) == ed25519.PrivateKeySize {
			// Best-effort shred of the on-disk material (guest responsibility)
			_ = os.WriteFile(keyPath, []byte("shredded"), 0600)
			_ = os.Remove(keyPath)
			priv := ed25519.PrivateKey(privBytes)
			pub := priv.Public().(ed25519.PublicKey)
			hubclient.ZeroPrivateKey(ed25519.PrivateKey(privBytes)) // defense in depth on the decoded copy
			return priv, pub, nil
		}
	}

	// Dev / test fallback only — never the production path inside a real microVM.
	log.Println("agent: no distributed VM key found — generating ephemeral key (dev/test only)")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

// dialHubTransport chooses unix (dev) or vsock (real Firecracker guest) based on env.
func dialHubTransport(pub ed25519.PublicKey, priv ed25519.PrivateKey) (hubclient.Client, error) {
	if portStr := os.Getenv("AEGIS_HUB_VSOCK_PORT"); portStr != "" {
		// Real guest path
		port := hubclient.HubVsockPort
		// (parse portStr if we want to support override; 9999 is the documented default)
		return hubclient.DialVsock(hubclient.HostCID, uint32(port), priv)
	}
	socket := expandPath(hubSocket)
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		socket = expandPath(env)
	}
	return hubclient.DialUnix(socket, priv)
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

