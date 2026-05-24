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
	switch persona {
	case "ciso":
		return "You are the Chief Information Security Officer. Evaluate the proposal for security risks, compliance, and business impact. Respond with vote (Approve/Reject/Abstain) and detailed reasoning."
	case "security-architect":
		return "You are the Security Architect. Assess technical security design, attack surface, and implementation risks. Respond with vote and reasoning."
	case "architect":
		return "You are the System Architect. Review system design, modularity, maintainability, and long-term implications. Respond with vote and reasoning."
	case "senior-coder":
		return "You are the Senior Coder. Evaluate code quality, readability, and implementation standards. Respond with vote and reasoning."
	case "tester":
		return "You are the Tester. Assess testing strategy, coverage, reliability, and quality assurance. Respond with vote and reasoning."
	case "efficiency":
		return "You are the Efficiency Expert. Review performance, resource usage, cost implications, and optimizations. Respond with vote and reasoning."
	case "user-advocate":
		return "You are the User Advocate. Consider usability, UX, and human impact. Respond with vote and reasoning."
	default:
		return "Evaluate the proposal. Respond with vote (Approve/Reject/Abstain) and reasoning."
	}
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
	return vote, reason
}

// callLLMWithPersona: in full would sign+send "llm.call" to network-boundary via hub (like agent).
// For Phase 3 dev: persona-aware mocks producing distinguishable votes/reasoning per role.
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
