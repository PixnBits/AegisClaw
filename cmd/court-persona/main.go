package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"AegisClaw/internal/eventbus"
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

func getPersonaPrompt(persona string) string {
	// 7.6: Prepend user workspace customizations (SOUL + AGENTS) if present.
	// This allows custom instructions to influence how the 7 personas review proposals,
	// consistent with the Agent 6-step integration and agent-customization.md.
	custom := ""
	if loadedWorkspace != nil {
		if loadedWorkspace.SOUL != "" {
			custom += "Core values and soul for this system: " + loadedWorkspace.SOUL + ". "
		}
		if loadedWorkspace.AGENTS != "" {
			custom += "Custom agent/Court instructions: " + loadedWorkspace.AGENTS + ". "
		}
	}

	base := ""
	switch persona {
	case "ciso":
		base = "You are the Chief Information Security Officer. Evaluate the proposal for security risks, compliance, and business impact. Respond with vote (Approve/Reject/Abstain) and detailed reasoning."
	case "security-architect":
		base = "You are the Security Architect. Assess technical security design, attack surface, and implementation risks. Respond with vote and reasoning."
	case "architect":
		base = "You are the System Architect. Review system design, modularity, maintainability, and long-term implications. Respond with vote and reasoning."
	case "senior-coder":
		base = "You are the Senior Coder. Evaluate code quality, readability, and implementation standards. Respond with vote and reasoning."
	case "tester":
		base = "You are the Tester. Assess testing strategy, coverage, reliability, and quality assurance. Respond with vote and reasoning."
	case "efficiency":
		base = "You are the Efficiency Expert. Review performance, resource usage, cost implications, and optimizations. Respond with vote and reasoning."
	case "user-advocate":
		base = "You are the User Advocate. Consider usability, UX, and human impact. Respond with vote and reasoning."
	default:
		base = "Evaluate the proposal. Respond with vote (Approve/Reject/Abstain) and reasoning."
	}

	return custom + base
}

func analyzeProposal(persona, proposalDesc string) (string, string) {
	prompt := getPersonaPrompt(persona) + "\n\nProposal: " + proposalDesc
	llmResponse := callLLMWithPersona(persona, prompt)
	// Parse for vote (encourage Abstain on uncertainty per spec)
	vote := "Approve"
	reason := llmResponse
	if strings.Contains(llmResponse, "Reject") || strings.Contains(llmResponse, "reject") {
		vote = "Reject"
	} else if strings.Contains(llmResponse, "Abstain") || strings.Contains(llmResponse, "abstain") || proposalDesc == "" {
		vote = "Abstain"
		reason = "Abstained: insufficient context or high uncertainty from " + persona + " perspective. " + llmResponse
	}
	// Structured-ish: include specific_feedback stub
	if vote != "Approve" {
		reason += " | specific_feedback: [Review details from Store; propose minimal changes if needed]"
	}

	// 7.2: Publish local decision event (in-process). In full system this (or a
	// signed version) flows through AegisHub so approval queues and proactive
	// agents can react.
	localBus := eventbus.New()
	localBus.PublishJSON("court.decision.made", map[string]interface{}{
		"persona": persona,
		"vote":    vote,
		"reason":  reason,
	}, eventbus.WithSource("court-persona"))

	return vote, reason
}

// callLLMWithPersona: in full would sign+send "llm.call" to network-boundary via hub (like agent).
// For Phase 3 dev: persona-aware mocks producing distinguishable votes/reasoning per role.
//
// 7.2 integration: After producing a decision/vote, publish via EventBus
// (see internal/eventbus ApprovalDecision + "court.decision.made" or "approval.decision").
// This drives approval queues and proactive agent reactions (autonomy/teams).
func callLLMWithPersona(persona, prompt string) string {
	lower := strings.ToLower(prompt)
	switch persona {
	case "ciso":
		if strings.Contains(lower, "skill") {
			return "Approve: Low strategic risk; aligns with compliance. specific_feedback: monitor post-deploy."
		}
		return "Abstain: Need more business context."
	case "security-architect":
		if strings.Contains(lower, "network") || strings.Contains(lower, "discord") {
			return "Reject: Expands attack surface via new outbound; needs policy gate."
		}
		return "Approve: Design sound if Builder gates pass."
	case "tester":
		if strings.Contains(lower, "test") {
			return "Approve: Good test plan implied."
		}
		return "Abstain: Test strategy not detailed in proposal."
	default:
		if strings.Contains(lower, "reject") {
			return "Reject: " + persona + " flags issues."
		}
		return "Approve: " + persona + " perspective satisfied for this change."
	}
}

func runCourtPersona(cmd *cobra.Command, args []string) {
	// Generate keys
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubStr := base64.StdEncoding.EncodeToString(pub)

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

	socket := expandPath(hubSocket)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		log.Fatal("Failed to connect to AegisHub:", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Register with unique source per persona (enables 7 distinct via scribe forwarding + ACL wildcards)
	uniqueSource := "court-persona-" + persona
	regMsg := Message{
		Source:      uniqueSource,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pubStr, "version": getBuildVersion()},
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	signMessage(&regMsg, priv)
	err = encoder.Encode(regMsg)
	if err != nil {
		log.Fatal("Failed to register:", err)
	}

	// Consume response
	var resp map[string]interface{}
	err = decoder.Decode(&resp)
	if err != nil {
		log.Fatal("Failed to decode register response:", err)
	}
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	fmt.Println("Court Persona", persona, "registered as", uniqueSource)

	// Persona loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Persona", persona, "received:", msg.Command)

		if msg.Command == "scribe.notify_review" {
			// Get proposal details from store (ID only; per spec Court pulls content directly)
			payload := msg.Payload.(map[string]interface{})
			proposalID := payload["proposal_id"].(string)
			getMsg := Message{
				Source:      uniqueSource,
				Destination: "store",
				Command:     "proposal.get",
				Payload:     map[string]interface{}{"id": proposalID},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&getMsg, priv)
			err = encoder.Encode(getMsg)
			if err != nil {
				log.Println("Failed to get proposal:", err)
				continue
			}
			var resp Message
			err = decoder.Decode(&resp)
			if err != nil {
				log.Println("Failed to decode proposal response:", err)
				continue
			}
			proposalData := resp.Payload.(map[string]interface{})
			description := ""
			if d, ok := proposalData["description"].(string); ok {
				description = d
			}

			// Analyze with persona-specific LLM (via Hub or fallback mock)
			vote, reasoning := analyzeProposal(persona, description)

			// Submit vote (signed, unique source)
			voteMsg := Message{
				Source:      uniqueSource,
				Destination: "court-scribe",
				Command:     "scribe.submit_vote",
				Payload: map[string]interface{}{
					"proposal_id": proposalID,
					"persona":     persona,
					"vote":        vote,
					"reasoning":   reasoning,
				},
				Timestamp: time.Now().Format(time.RFC3339),
				Signature: "",
			}
			signMessage(&voteMsg, priv)
			err = encoder.Encode(voteMsg)
			if err != nil {
				log.Println("Failed to submit vote:", err)
			}
		} else if msg.Command == "version" || msg.Command == "get-version" {
			response := Message{
				Source:      uniqueSource,
				Destination: msg.Source,
				Command:     "version",
				Payload:     map[string]string{"version": getBuildVersion()},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&response, priv)
			err = encoder.Encode(response)
			if err != nil {
				log.Println("Failed to send version response:", err)
			}
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
