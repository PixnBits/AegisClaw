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
	"strings"

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
	llmResponse := callLLM(prompt)
	// Parse response for vote and reasoning (simple simulation)
	if strings.Contains(llmResponse, "Reject") {
		return "Reject", llmResponse
	} else if strings.Contains(llmResponse, "Abstain") {
		return "Abstain", llmResponse
	} else {
		return "Approve", llmResponse
	}
}

func callLLM(prompt string) string {
	// Mock LLM response
	return "Approve: " + prompt
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

	// Register
	regMsg := Message{
		Source:      "court-" + persona,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pubStr},
		Timestamp:   "2026-05-09T19:50:00Z",
		Signature:   "dummy",
	}
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
	fmt.Println("Court Persona", persona, "registered")

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
			// Get proposal details from store
			payload := msg.Payload.(map[string]interface{})
			proposalID := payload["proposal_id"].(string)
			getMsg := Message{
				Source:      "court-" + persona,
				Destination: "store",
				Command:     "proposal.get",
				Payload:     map[string]interface{}{"id": proposalID},
				Timestamp:   "2026-05-09T19:50:01Z",
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
			description := proposalData["description"].(string)

			// Analyze with LLM
			vote, reasoning := analyzeProposal(persona, description)

			// Submit vote
			voteMsg := Message{
				Source:      "court-" + persona,
				Destination: "court-scribe",
				Command:     "scribe.submit_vote",
				Payload: map[string]interface{}{
					"proposal_id": proposalID,
					"persona":     persona,
					"vote":        vote,
					"reasoning":   reasoning,
				},
				Timestamp: "2026-05-09T19:50:01Z",
				Signature: "",
			}
			signMessage(&voteMsg, priv)
			err = encoder.Encode(voteMsg)
			if err != nil {
				log.Println("Failed to submit vote:", err)
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
