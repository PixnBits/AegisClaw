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
	base := "You are the Project Manager Agent. You receive user goals or channel activity. " +
		"Break them into plans (tasks, required roles like Coder/Tester/Court, suggested channels). " +
		"Decide which agents/roles to spin up or invite to which channels using EnsureRoleAgent. " +
		"Delegate via channel posts or @mentions. Monitor, synthesize, and escalate to Court via formal proposals when changes are needed. " +
		"Stay in character as the intelligent orchestrator. Respond with structured plans or actions."
	return custom + base
}

// extractChannelFromPayload centralizes the channel hint logic used by PM.
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

// extractRolesFromText makes role delegation richer (used after LLM or fallback plan).
// Always includes baseline coder/tester; scans text for common Court/SDLC keywords
// so the PM can dynamically involve the right personas based on the generated plan content.
func extractRolesFromText(text string) []string {
	roles := []string{"coder", "tester"}
	lower := strings.ToLower(text)
	candidates := map[string]string{
		"ciso":               "ciso",
		"security":           "ciso",
		"security-architect": "security-architect",
		"architect":          "architect",
		"senior-coder":       "senior-coder",
		"efficiency":         "efficiency",
		"user-advocate":      "user-advocate",
		"court":              "ciso", // broad -> at least security
	}
	for key, role := range candidates {
		if strings.Contains(lower, key) {
			found := false
			for _, r := range roles {
				if r == role {
					found = true
					break
				}
			}
			if !found {
				roles = append(roles, role)
			}
		}
	}
	return roles
}

func generatePlan(input, chID string) string {
	base := getPMPrompt() + "\n\nInput: " + input + "\n\nChannel: " + chID + "\n\nStructured Plan:\n"
	plan := base + "1. Analyze the goal and break into tasks.\n2. Identify required roles (e.g. Coder, Tester, Court for changes).\n3. Create/use channel and ensure roles (default PM included).\n4. Delegate via @mentions and channel posts.\n5. Monitor progress and synthesize results.\n6. Escalate formal proposal to Court if needed.\n"
	lower := strings.ToLower(input)
	if strings.Contains(lower, "feature") || strings.Contains(lower, "code") || strings.Contains(lower, "implement") {
		plan += "- Specific: Coder implements core logic; Tester adds tests and validates.\n"
	}
	if strings.Contains(lower, "test") || strings.Contains(lower, "validate") {
		plan += "- Emphasis on Tester role for coverage and edge cases.\n"
	}
	if strings.Contains(lower, "security") || strings.Contains(lower, "risk") {
		plan += "- Include Court (CISO, Security Architect) for review gate.\n"
	}
	return plan
}

// pmProcessPlanningMessage runs LLM planning, channel.post, and ensure.role delegation.
// Must not run synchronously inside the Receive loop for hub RPC-delivered user.goal
// (see user.goal case: immediate Reply + goroutine).
func pmProcessPlanningMessage(hcl hubclient.Client, msg hubclient.Message, uniqueSource string, realLLM agent.LLMCallFunc) {
	payloadStr := fmt.Sprintf("%v", msg.Payload)
	chID := extractChannelFromPayload(msg.Payload, "plan-demo")

	if strings.Contains(payloadStr, uniqueSource) && msg.Command != "user.goal" {
		ack := hubclient.Message{
			Source:      uniqueSource,
			Destination: "store",
			Command:     "channel.post",
			Payload: map[string]interface{}{
				"channel_id": chID,
				"from":       uniqueSource,
				"content":    "PM: noted own update; continuing to monitor channel activity.",
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		_, _ = hcl.Send(context.Background(), ack)
		return
	}

	if msg.Command == "channel.post" {
		from := "unknown"
		if p, ok := msg.Payload.(map[string]interface{}); ok {
			if f, ok := p["from"].(string); ok && f != "" {
				from = f
			}
		}
		if from != uniqueSource {
			note := fmt.Sprintf("PM: noted activity from %s in channel %s. Monitoring for progress or escalation needs.", from, chID)
			_, _ = hcl.Send(context.Background(), hubclient.Message{
				Source:      uniqueSource,
				Destination: "store",
				Command:     "channel.post",
				Payload: map[string]interface{}{
					"channel_id": chID,
					"from":       uniqueSource,
					"content":    note,
				},
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			fmt.Printf("PM: posted monitoring note for activity from %s\n", from)
			return
		}
	}

	var plan string
	planPrompt := getPMPrompt() + "\n\nUser goal: " + payloadStr + "\n\nChannel: " + chID + "\n\nAs Project Manager, output a clear structured plan with tasks, roles to ensure (Coder, Tester, Court etc.), delegation steps, and monitoring. Be actionable."
	llmPlan, err := realLLM(context.Background(), planPrompt)
	if err != nil {
		log.Printf("PM: LLM plan gen failed (%v), using fallback generatePlan", err)
		plan = generatePlan(payloadStr, chID)
	} else {
		plan = llmPlan
		log.Printf("PM: LLM plan gen succeeded (model=%s, chars=%d)", bootargs.DefaultModel(agent.DefaultLLMModel), len(plan))
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

	monitorContent := fmt.Sprintf("PM monitoring: roles ensured %v in channel %s. Awaiting updates from roles; will synthesize and escalate to Court when needed.", rolesToEnsure, chID)
	if _, err := hcl.Send(context.Background(), hubclient.Message{
		Source:      uniqueSource,
		Destination: "store",
		Command:     "channel.post",
		Payload: map[string]interface{}{
			"channel_id": chID,
			"from":       uniqueSource,
			"content":    monitorContent,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		log.Printf("pm: monitoring post failed: %v", err)
	} else {
		fmt.Printf("PM: posted monitoring update to channel %s\n", chID)
	}
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

	llmModel := bootargs.DefaultModel(agent.DefaultLLMModel)
	realLLM := loop.NewRealLLMCaller(hcl, llmModel)

	timing.RecordPhase("message_loop_ready")

	for {
		msg, err := hcl.Receive(context.Background())
		if err != nil {
			log.Println("pm: hub Receive error (continuing):", err)
			continue
		}

		fmt.Println("PM received:", msg.Command)

		switch msg.Command {
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
