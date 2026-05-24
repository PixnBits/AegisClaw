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

func callLLM(prompt string, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) string {
	// Send LLM request to Network Boundary
	model := "qwen3-coder:30b"
	if envModel := strings.TrimSpace(os.Getenv("AEGIS_DEFAULT_MODEL")); envModel != "" {
		model = envModel
	}
	llmRequest := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	llmMsg := Message{
		Source:      "agent",
		Destination: "network-boundary",
		Command:     "llm.call",
		Payload:     map[string]interface{}{"request": llmRequest, "endpoint": "/api/generate"},
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "",
	}
	signMessage(&llmMsg, priv)
	err := encoder.Encode(llmMsg)
	if err != nil {
		return fmt.Sprintf("Error: Failed to send LLM request: %v", err)
	}

	// Wait for response
	var respMsg Message
	err = decoder.Decode(&respMsg)
	if err != nil {
		return fmt.Sprintf("Error: Failed to receive LLM response: %v", err)
	}

	if respMsg.Command == "llm.response" {
		if payload, ok := respMsg.Payload.(map[string]interface{}); ok {
			if response, ok := payload["response"].(string); ok {
				// Parse the Ollama response JSON
				var ollamaResp map[string]interface{}
				if err := json.Unmarshal([]byte(response), &ollamaResp); err == nil {
					if text, ok := ollamaResp["response"].(string); ok {
						return text
					}
				}
				return response // Return raw if parsing fails
			}
			if error, ok := payload["error"].(string); ok {
				return fmt.Sprintf("LLM Error: %s", error)
			}
		}
	}

	return "Error: Invalid LLM response format"
}

// Fallback for dev / when Network Boundary not fully wired yet
func callLLMWithFallback(prompt string, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) string {
	resp := callLLM(prompt, encoder, decoder, priv)
	if strings.HasPrefix(resp, "Error:") || strings.HasPrefix(resp, "LLM Error") {
		return mockLLMResponse(prompt)
	}
	return resp
}

func observe(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	input := fmt.Sprintf("%v", msg.Payload)
	prompt := "Observe and parse the user/agent request. Extract intent, key entities, and whether this requires a proposal (e.g. new skill). Input: " + input + ". Return structured observation."
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("1. Observe:", llmResponse)

	// Get context from memory (per agent-runtime.md + memory-vm.md)
	contextMsg := Message{
		Source:      "agent",
		Destination: "memory",
		Command:     "memory.get_context",
		Payload:     map[string]interface{}{"reason": "observe step"},
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "",
	}
	signMessage(&contextMsg, priv)
	err := encoder.Encode(contextMsg)
	if err != nil {
		fmt.Println("Failed to get context:", err)
		return
	}

	// Wait for response (hub routes back)
	var contextResp Message
	err = decoder.Decode(&contextResp)
	if err != nil {
		fmt.Println("Failed to decode context:", err)
		return
	}
	fmt.Println("Context received (short-term + relevant long-term):", contextResp.Payload)
}

func think(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	input := fmt.Sprintf("%v", msg.Payload)
	prompt := "Think step-by-step about the observed request using prior context. Identify risks, required skills/tools, autonomy implications. Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("2. Think:", llmResponse)
}

func plan(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	input := fmt.Sprintf("%v", msg.Payload)
	prompt := "Create a concrete plan: steps, which tools/skills via Hub, whether to create a formal proposal for Court review (per governance-court.md). Be specific. Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("3. Plan:", llmResponse)
}

func act(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	input := fmt.Sprintf("%v", msg.Payload)
	prompt := "Execute the 'Act' phase: prepare specific tool invocations (signed via Hub) or proposal payload. If skill creation, prepare for proposal.create. Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("4. Act:", llmResponse)
}

func execute(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	input := fmt.Sprintf("%v", msg.Payload)
	prompt := "Perform the execution: actually send signed tool/skill calls to Hub or invoke proposal creation flow. Capture results. Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("5. Execute:", llmResponse)
}

func judge(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	llmResponse := callLLMWithFallback("Judge the response quality, compliance with policy, and whether Court review is required. Payload: "+fmt.Sprintf("%v", msg.Payload), encoder, decoder, priv)
	fmt.Println("6. Judge:", llmResponse)

	// If the request is to add a skill, create a proposal (triggers Court per Phase 3 / governance-court.md)
	payloadStr := fmt.Sprintf("%v", msg.Payload)
	if strings.Contains(strings.ToLower(payloadStr), "add a") && strings.Contains(strings.ToLower(payloadStr), "skill") {
		createProposal(payloadStr, encoder, decoder, priv)
	}
}

func mockLLMResponse(prompt string) string {
	lower := strings.ToLower(prompt)
	isSkill := strings.Contains(lower, "skill") || strings.Contains(lower, "add a")
	if strings.Contains(prompt, "Observe") || strings.Contains(lower, "observe and parse") {
		if isSkill {
			return "Observed: Intent='create new skill'. Entities: name, perms, code. Requires Court proposal. Context: prior conv empty."
		}
		return "Observed: General request. Loaded recent context + 2 long-term memories."
	} else if strings.Contains(prompt, "Think") || strings.Contains(lower, "think step-by-step") {
		if isSkill {
			return "Thought: New skill increases attack surface; must go through all 7 personas + Builder gates. Low autonomy change."
		}
		return "Thought: Straightforward Q&A or tool use. No governance trigger."
	} else if strings.Contains(prompt, "Plan") || strings.Contains(lower, "create a concrete plan") {
		if isSkill {
			return "Plan: 1. Extract spec via LLM. 2. proposal.create to Store. 3. scribe.notify_review (ID only). 4. Await Court votes. 5. On approve, Builder."
		}
		return "Plan: Answer directly or call 1-2 tools via Hub."
	} else if strings.Contains(prompt, "Act") || strings.Contains(lower, "execute the 'act' phase") {
		return "Acted: Prepared proposal payload or tool call list."
	} else if strings.Contains(prompt, "Execute") || strings.Contains(lower, "perform the execution") {
		if isSkill {
			return "Executed: Sent signed proposal.create + scribe notify (ID only) to Hub."
		}
		return "Executed: Tool results received and merged into response."
	} else if strings.Contains(prompt, "Judge") || strings.Contains(lower, "judge the response quality") {
		if isSkill {
			return "Judged: Proposal ready for Court. Quality good; unanimous-approve path expected for trivial skill."
		}
		return "Judged: High quality, safe, no further action. Stored summary to Memory."
	}
	return "LLM response: " + prompt
}

func createProposal(description string, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey) {
	// Use LLM to extract skill specs (full details go to Store only)
	prompt := "Extract skill name, description, required permissions, and code skeleton from: " + description
	extracted := callLLMWithFallback(prompt, encoder, decoder, priv)
	proposalID := "proposal_" + fmt.Sprintf("%d", time.Now().Unix())
	proposal := map[string]interface{}{
		"id":          proposalID,
		"description": description,
		"extracted":   extracted,
		"status":      "pending",
	}
	msg := Message{
		Source:      "agent",
		Destination: "store",
		Command:     "proposal.create",
		Payload:     proposal,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "",
	}
	signMessage(&msg, priv)
	encoder.Encode(msg)
	fmt.Println("Proposal created:", proposalID)

	// Notify Court Scribe **with ID only** (per court-scribe.md: Scribe must never see or transmit proposal content/text. Personas fetch from Store.)
	scribeMsg := Message{
		Source:      "agent",
		Destination: "court-scribe",
		Command:     "scribe.notify_review",
		Payload:     map[string]interface{}{"proposal_id": proposalID},
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "",
	}
	signMessage(&scribeMsg, priv)
	encoder.Encode(scribeMsg)
	fmt.Println("Notified Court Scribe for proposal review (ID only, no content)")
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
		Source:      "agent",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: "2026-05-09T19:30:00Z",
		Signature: "dummy", // Register doesn't require sig
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

		// Log to file for debugging
		if f, err := os.OpenFile("/tmp/agent-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666); err == nil {
			fmt.Fprintf(f, "[%s] Received: %s from %s\n", time.Now().Format("15:04:05.000"), msg.Command, msg.Source)
			f.Close()
		}

		// Handle version queries
		if msg.Command == "version" || msg.Command == "get-version" {
			version := getBuildVersion()
			// For all version queries, send full Message
			response := Message{
				Source:      "agent",
				Destination: msg.Source,
				Command:     "version",
				Payload:     map[string]string{"version": version},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&response, priv)
			if f, err := os.OpenFile("/tmp/agent-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666); err == nil {
				fmt.Fprintf(f, "[%s] Sending version response: %s to %s (cmd=%s)\n", time.Now().Format("15:04:05.000"), version, msg.Source, msg.Command)
				f.Close()
			}
			encoder.Encode(response)
			continue
		}

		// 6-step loop
		observe(&msg, encoder, decoder, priv)
		think(&msg, encoder, decoder, priv)
		plan(&msg, encoder, decoder, priv)
		act(&msg, encoder, decoder, priv)
		execute(&msg, encoder, decoder, priv)
		judge(&msg, encoder, decoder, priv)

		// Respond
		response := Message{
			Source:      "agent",
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
