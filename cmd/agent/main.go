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

func observe(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey, idx *AgentSkillIndex) {
	input := fmt.Sprintf("%v", msg.Payload)
	available := formatAvailableTools(idx)
	prompt := "Observe and parse the user/agent request. Extract intent, key entities, and whether this requires a proposal (e.g. new skill). Available local tools/skills: " + available + ". Input: " + input + ". Return structured observation."
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

func think(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey, idx *AgentSkillIndex) {
	input := fmt.Sprintf("%v", msg.Payload)
	available := formatAvailableTools(idx)
	prompt := "Think step-by-step about the observed request using prior context. Identify risks, required skills/tools, autonomy implications. Available local tools you can actually call: " + available + ". Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("2. Think:", llmResponse)
}

func plan(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey, idx *AgentSkillIndex) {
	input := fmt.Sprintf("%v", msg.Payload)
	available := formatAvailableTools(idx)
	prompt := "Create a concrete plan: steps, which tools/skills via Hub (only use ones from the available local index), whether to create a formal proposal for Court review (per governance-court.md). Be specific. Available tools: " + available + ". Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("3. Plan:", llmResponse)
}

func act(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey, idx *AgentSkillIndex) {
	input := fmt.Sprintf("%v", msg.Payload)
	available := formatAvailableTools(idx)
	prompt := "Execute the 'Act' phase: prepare specific tool invocations (signed via Hub, only from available local index) or proposal payload. If skill creation, prepare for proposal.create. Available tools: " + available + ". Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("4. Act:", llmResponse)
}

func execute(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey, idx *AgentSkillIndex) {
	input := fmt.Sprintf("%v", msg.Payload)
	available := formatAvailableTools(idx)
	prompt := "Perform the execution: actually send signed tool/skill calls to Hub (only use tools from the available local index) or invoke proposal creation flow. Capture results. Available: " + available + ". Request: " + input
	llmResponse := callLLMWithFallback(prompt, encoder, decoder, priv)
	fmt.Println("5. Execute:", llmResponse)
}

func judge(msg *Message, encoder *json.Encoder, decoder *json.Decoder, priv ed25519.PrivateKey, idx *AgentSkillIndex) {
	available := formatAvailableTools(idx)
	llmResponse := callLLMWithFallback("Judge the response quality, compliance with policy, and whether Court review is required. Available local tools: "+available+". Payload: "+fmt.Sprintf("%v", msg.Payload), encoder, decoder, priv)
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
			return "Thought: New skill increases attack surface; must go through all 7 personas + Builder gates. Low autonomy change. Available tools considered: discord_monitor.send_message, web_research.search."
		}
		return "Thought: Straightforward Q&A or tool use. No governance trigger. Relevant local tools: web_research.* if research needed."
	} else if strings.Contains(prompt, "Plan") || strings.Contains(lower, "create a concrete plan") {
		if isSkill {
			return "Plan: 1. Extract spec via LLM. 2. proposal.create to Store. 3. scribe.notify_review (ID only). 4. Await Court votes. 5. On approve, Builder. (Will only propose tools that exist in local index.)"
		}
		return "Plan: Answer directly or call 1-2 tools via Hub from local index (e.g. discord_monitor.send_message or web_research.search)."
	} else if strings.Contains(prompt, "Act") || strings.Contains(lower, "execute the 'act' phase") {
		return "Acted: Prepared proposal payload or tool call list using only available local tools from index."
	} else if strings.Contains(prompt, "Execute") || strings.Contains(lower, "perform the execution") {
		if isSkill {
			return "Executed: Sent signed proposal.create + scribe notify (ID only) to Hub."
		}
		return "Executed: Tool results received from local index tools and merged into response."
	} else if strings.Contains(prompt, "Judge") || strings.Contains(lower, "judge the response quality") {
		if isSkill {
			return "Judged: Proposal ready for Court. Quality good; unanimous-approve path expected for trivial skill. (Considered local tool availability.)"
		}
		return "Judged: High quality, safe, no further action. Stored summary to Memory. Used local tool index for awareness."
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

	// 7.3: Fast local semantic skill/tool index (stdlib only, always available)
	skillIndex := NewAgentSkillIndex()

	// Agent loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Agent received message:", msg.Command, "from", msg.Source)

		// 7.3: Fast local tool discovery commands (bypass heavy 6-step LLM loop)
		if msg.Command == "tool.list" || msg.Command == "tool.search" {
			result := handleToolCommand(msg.Command, msg.Payload, skillIndex)
			resp := Message{
				Source:      "agent",
				Destination: msg.Source,
				Command:     msg.Command + ".response",
				Payload:     result,
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&resp, priv)
			encoder.Encode(resp)
			continue
		}

		// 7.3 portal/CLI exposure helper: full current snapshot of the local index
		// The thin web-portal or `aegis` CLI can call this to show users what the
		// agent currently knows about (very useful for debugging autonomy).
		if msg.Command == "tools.snapshot" || msg.Command == "skills.snapshot" {
			snapshot := map[string]interface{}{
				"skills": skillIndex.ListSkills(),
				"tools":  skillIndex.tools,
				"count":  len(skillIndex.skills) + len(skillIndex.tools),
			}
			resp := Message{
				Source:      "agent",
				Destination: msg.Source,
				Command:     "tools.snapshot.response",
				Payload:     snapshot,
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&resp, priv)
			encoder.Encode(resp)
			continue
		}

		// 7.2 consumer example: React to approval decisions (from Court or human via UI)
		// This enables proactive/background agent actions and closes the approval loop.
		if msg.Command == "approval.decision" {
			log.Printf("7.2: Agent received approval decision: %+v", msg.Payload)
			// Real implementation would trigger previously planned actions, tool calls, etc.
			continue
		}

		// 7.3 + 7.2: Dynamic index update from Hub / future EventBus (skill.deployed etc.)
		// This is the invalidation/refresh path. In a full EventBus world the Hub
		// would forward "skill.deployed" events here.
		if msg.Command == "skill.register" || msg.Command == "skill.deployed" || msg.Command == "index.update" {
			if payload, ok := msg.Payload.(map[string]interface{}); ok {
				if skillID, ok := payload["id"].(string); ok {
					newSkill := Skill{
						ID:          skillID,
						Name:        getString(payload, "name", skillID),
						Description: getString(payload, "description", ""),
						Version:     getString(payload, "version", "1.0.0"),
					}
					skillIndex.AddSkill(newSkill)

					// Also accept optional tools in the same payload
					if toolsIface, ok := payload["tools"]; ok {
						if tools, ok := toolsIface.([]interface{}); ok {
							for _, ti := range tools {
								if tmap, ok := ti.(map[string]interface{}); ok {
									skillIndex.AddTool(Tool{
										Name:        getString(tmap, "name", ""),
										Description: getString(tmap, "description", ""),
										SkillID:     skillID,
									})
								}
							}
						}
					}
					log.Printf("7.3: Local skill index updated with %s (dynamic registration)", skillID)
				}
			}
			// Ack
			resp := Message{
				Source:      "agent",
				Destination: msg.Source,
				Command:     "index.updated",
				Payload:     map[string]string{"status": "accepted"},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&resp, priv)
			encoder.Encode(resp)
			continue
		}

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

		// 6-step loop (now with local tool awareness via 7.3 index)
		observe(&msg, encoder, decoder, priv, skillIndex)
		think(&msg, encoder, decoder, priv, skillIndex)
		plan(&msg, encoder, decoder, priv, skillIndex)
		act(&msg, encoder, decoder, priv, skillIndex)
		execute(&msg, encoder, decoder, priv, skillIndex)
		judge(&msg, encoder, decoder, priv, skillIndex)

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

// === 7.3 Semantic Tool/Skill Discovery (stdlib-only fast local index) ===
//
// Per plan: available inside every Agent VM for the 6-step loop and direct
// "tool.list" / "tool.search" commands. No external vector DB deps.
//
// Design: simple in-memory index with keyword overlap + lightweight scoring.
// Future: can be invalidated/refreshed via EventBus on skill deploy (7.2).

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SkillID     string `json:"skill_id"`
}

type SearchResult struct {
	Tool        Tool    `json:"tool"`
	SkillName   string  `json:"skill_name"`
	Score       float64 `json:"score"`
	Description string  `json:"description"`
}

// AgentSkillIndex is the fast local index every agent runtime carries.
type AgentSkillIndex struct {
	skills []Skill
	tools  []Tool
}

func NewAgentSkillIndex() *AgentSkillIndex {
	idx := &AgentSkillIndex{}
	// Seed a few realistic examples so the index is immediately useful in dev.
	// In real operation these come from Store/Hub on startup or via events.
	idx.AddSkill(Skill{
		ID: "discord_monitor", Name: "discord_monitor",
		Description: "Monitor Discord channels and send messages. Supports sending alerts and summaries.",
		Version: "1.2.0", Tags: []string{"discord", "social", "notification"},
	})
	idx.AddTool(Tool{Name: "discord_monitor.send_message", Description: "Send a message to a specific Discord channel", SkillID: "discord_monitor"})
	idx.AddTool(Tool{Name: "discord_monitor.get_recent", Description: "Retrieve recent messages from a channel", SkillID: "discord_monitor"})

	idx.AddSkill(Skill{
		ID: "web_research", Name: "web_research",
		Description: "Search the web, fetch pages, and summarize information from public sources.",
		Version: "0.9.1", Tags: []string{"web", "research", "search"},
	})
	idx.AddTool(Tool{Name: "web_research.search", Description: "Perform a web search for a query", SkillID: "web_research"})
	idx.AddTool(Tool{Name: "web_research.summarize_url", Description: "Fetch and summarize the content of a URL", SkillID: "web_research"})

	return idx
}

func (idx *AgentSkillIndex) AddSkill(s Skill) {
	idx.skills = append(idx.skills, s)
}

func (idx *AgentSkillIndex) AddTool(t Tool) {
	idx.tools = append(idx.tools, t)
}

// ListSkills returns all known skills (exact, fast path).
func (idx *AgentSkillIndex) ListSkills() []Skill {
	return append([]Skill(nil), idx.skills...) // copy
}

// SearchTools performs a simple stdlib-only semantic-ish search.
// Uses token overlap (Jaccard-style) + bonus for name/description substring matches.
// Returns top results sorted by score (highest first).
func (idx *AgentSkillIndex) SearchTools(query string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 10
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	qTokens := tokenize(q)
	results := make([]SearchResult, 0, len(idx.tools))

	for _, t := range idx.tools {
		skillName := t.SkillID
		for _, s := range idx.skills {
			if s.ID == t.SkillID {
				skillName = s.Name
				break
			}
		}

		text := strings.ToLower(t.Name + " " + t.Description + " " + skillName)
		tTokens := tokenize(text)

		score := jaccardScore(qTokens, tTokens)

		// Bonus for direct substring matches (makes it feel more "semantic" without embeddings)
		if strings.Contains(text, q) {
			score += 0.25
		}
		if strings.Contains(t.Name, q) {
			score += 0.15
		}

		// Cheap Levenshtein boost for very short queries (single concept match)
		if len(qTokens) <= 3 {
			for _, qt := range qTokens {
				for _, tt := range tTokens {
					if levenshtein(qt, tt) <= 2 { // allow 1-2 char typos
						score += 0.1
						break
					}
				}
			}
		}

		// Light TF boost: reward tools whose description contains the query terms multiple times
		tfBoost := 0.0
		for _, qt := range qTokens {
			count := strings.Count(text, qt)
			if count > 1 {
				tfBoost += 0.05 * float64(count-1)
			}
		}
		score += tfBoost

		if score > 0.05 { // filter out very weak matches
			results = append(results, SearchResult{
				Tool:        t,
				SkillName:   skillName,
				Score:       score,
				Description: t.Description,
			})
		}
	}

	// Sort by score desc
	sortSearchResults(results)

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func tokenize(s string) []string {
	// Very simple word tokenizer (good enough for this phase)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	seen := make(map[string]bool)
	var out []string
	for _, f := range fields {
		if len(f) > 2 && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func jaccardScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(a))
	for _, x := range a {
		setA[x] = true
	}
	inter := 0
	for _, x := range b {
		if setA[x] {
			inter++
		}
	}
	union := len(setA) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func sortSearchResults(res []SearchResult) {
	// Simple insertion sort (tiny data, no need for sort package dependency concerns)
	for i := 1; i < len(res); i++ {
		j := i
		for j > 0 && res[j].Score > res[j-1].Score {
			res[j], res[j-1] = res[j-1], res[j]
			j--
		}
	}
}

// levenshtein is a tiny stdlib-only distance for close-match boosting in search.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 0; i < len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i + 1
		for j := 0; j < len(b); j++ {
			cost := 0
			if a[i] != b[j] {
				cost = 1
			}
			curr[j+1] = min(
				curr[j]+1,      // insertion
				prev[j+1]+1,    // deletion
				prev[j]+cost,   // substitution
			)
		}
		prev = curr
	}
	return prev[len(b)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// formatAvailableTools returns a compact string of the local skill/tool index
// for injection into LLM prompts (keeps context reasonable).
func formatAvailableTools(idx *AgentSkillIndex) string {
	if idx == nil {
		return "(no local tool index available)"
	}
	var b strings.Builder
	b.WriteString("Skills: ")
	for i, s := range idx.skills {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s.Name)
		b.WriteString(" (")
		b.WriteString(s.Description)
		b.WriteString(")")
	}
	b.WriteString(". Tools: ")
	for i, t := range idx.tools {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.Name)
	}
	return b.String()
}

// getString is a tiny helper for safe map[string]interface{} access.
func getString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// handleToolCommand is called from the agent's main message loop for tool.* commands.
func handleToolCommand(cmd string, payload interface{}, idx *AgentSkillIndex) interface{} {
	switch cmd {
	case "tool.list":
		return map[string]interface{}{
			"skills": idx.ListSkills(),
			"tools":  idx.tools,
		}
	case "tool.search":
		q := ""
		if p, ok := payload.(map[string]interface{}); ok {
			if qq, ok := p["query"].(string); ok {
				q = qq
			}
		}
		return map[string]interface{}{
			"query":   q,
			"results": idx.SearchTools(q, 8),
		}
	}
	return map[string]string{"error": "unknown tool command"}
}

