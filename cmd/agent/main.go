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

func observe(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	prompt := "Observe the user input: " + fmt.Sprintf("%v", msg.Payload)
	llmResponse := callLLM(prompt)
	fmt.Println("1. Observe:", llmResponse)

	// Get context from memory
	contextMsg := Message{
		Source:      "agent1",
		Destination: "memory",
		Command:     "memory.get_context",
		Payload:     nil,
		Timestamp:   "2026-05-09T19:30:02Z",
		Signature:   "",
	}
	signMessage(&contextMsg, priv)
	err := encoder.Encode(contextMsg)
	if err != nil {
		fmt.Println("Failed to get context:", err)
		return
	}

	// Wait for response
	var contextResp Message
	err = decoder.Decode(&contextResp)
	if err != nil {
		fmt.Println("Failed to decode context:", err)
		return
	}
	fmt.Println("Context received:", contextResp.Payload)
}

func think(msg *Message) {
	prompt := "Think about the request: " + fmt.Sprintf("%v", msg.Payload)
	llmResponse := callLLM(prompt)
	fmt.Println("2. Think:", llmResponse)
}

func plan(msg *Message) {
	prompt := "Plan how to respond to: " + fmt.Sprintf("%v", msg.Payload)
	llmResponse := callLLM(prompt)
	fmt.Println("3. Plan:", llmResponse)
}

func act(msg *Message) {
	prompt := "Act on the plan for: " + fmt.Sprintf("%v", msg.Payload)
	llmResponse := callLLM(prompt)
	fmt.Println("4. Act:", llmResponse)
}

func execute(msg *Message) {
	prompt := "Execute the actions for: " + fmt.Sprintf("%v", msg.Payload)
	llmResponse := callLLM(prompt)
	fmt.Println("5. Execute:", llmResponse)
}

func judge(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	llmResponse := callLLM("Judge the response quality: " + fmt.Sprintf("%v", msg.Payload))
	fmt.Println("6. Judge:", llmResponse)

	// If the request is to add a skill, create a proposal
	payloadStr := fmt.Sprintf("%v", msg.Payload)
	if strings.Contains(strings.ToLower(payloadStr), "add a") && strings.Contains(strings.ToLower(payloadStr), "skill") {
		createProposal(payloadStr, encoder, priv)
	}
}

// Mock LLM integration
func callLLM(prompt string) string {
	// Simulate LLM response based on prompt
	if strings.Contains(prompt, "Observe") {
		return "Observed: User input received and context loaded."
	} else if strings.Contains(prompt, "Think") {
		return "Analyzed: This is a request for information."
	} else if strings.Contains(prompt, "Plan") {
		return "Planned: Respond with relevant information."
	} else if strings.Contains(prompt, "Act") {
		return "Acting: Prepare tool calls if needed."
	} else if strings.Contains(prompt, "Execute") {
		return "Executed: Tools called and results received."
	} else if strings.Contains(prompt, "Judge") {
		return "Judged: Response quality is good."
	}
	return "LLM response: " + prompt
}

func createProposal(description string, encoder *json.Encoder, priv ed25519.PrivateKey) {
	proposal := map[string]interface{}{
		"id":          "proposal_" + fmt.Sprintf("%d", time.Now().Unix()),
		"description": description,
		"status":      "pending",
	}
	msg := Message{
		Source:      "agent1",
		Destination: "store",
		Command:     "proposal.create",
		Payload:     proposal,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "",
	}
	signMessage(&msg, priv)
	encoder.Encode(msg)
	fmt.Println("Proposal created:", proposal["id"])
}

func runAgent(cmd *cobra.Command, args []string) {
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
		Source:      "agent1",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pubStr},
		Timestamp:   "2026-05-09T19:30:00Z",
		Signature:   "dummy", // Register doesn't require sig
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
	fmt.Println("Agent registered")

	// Agent loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Agent received message:", msg.Command, "from", msg.Source)

		// 6-step loop
		observe(&msg, encoder, decoder, priv)
		think(&msg)
		plan(&msg)
		act(&msg)
		execute(&msg)
		judge(&msg, encoder, decoder, priv)

		// Respond
		response := Message{
			Source:      "agent1",
			Destination: msg.Source,
			Command:     "response",
			Payload:     "Agent processed: " + msg.Command,
			Timestamp:   "2026-05-09T19:30:01Z",
			Signature:   "",
		}
		signMessage(&response, priv)
		err = encoder.Encode(response)
		if err != nil {
			log.Println("Failed to send response:", err)
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "agent",
		Short: "Agent Runtime",
		Run:   runAgent,
	}

	rootCmd.Execute()
}
