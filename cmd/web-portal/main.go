package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

//go:embed static/*
var staticFiles embed.FS

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

type StreamMessage struct {
	Type      string                 `json:"type"`
	MessageID string                 `json:"message_id"`
	SessionID string                 `json:"session_id"`
	Timestamp string                 `json:"timestamp"`
	TraceID   string                 `json:"trace_id"`
	Content   map[string]interface{} `json:"content"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

type daemonSnapshot struct {
	Running    bool
	Backend    string
	SafeMode   bool
	RunningVMs int
	Hub        string
	MemoryVM   string
	StoreVM    string
}

var hubSocket = "~/.aegis/hub.sock"

func init() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocket = env
	}
}

var (
	initialResponseDelay = 200 * time.Millisecond
	thinkingDelay        = 300 * time.Millisecond
	toolResultDelay      = 200 * time.Millisecond
	finalResponseDelay   = 100 * time.Millisecond
	wordStreamDelay      = 50 * time.Millisecond
	defaultOllamaModel   = "qwen3-coder:30b"
	sessionIDPattern     = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

const (
	unknownStatus        = "unknown"
	ollamaRequestTimeout = 30 * time.Second
	daemonConnectTimeout = 400 * time.Millisecond
	daemonReadDeadline   = 1 * time.Second
)

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func connectToHub() (net.Conn, error) {
	socket := expandPath(hubSocket)
	return net.Dial("unix", socket)
}

func handleHubMessages() {
	for {
		conn, err := connectToHub()
		if err != nil {
			fmt.Printf("Failed to connect to hub: %v, retrying...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Dev keypair for web-portal (in real deployment this comes from daemon injection via the VM rootfs)
		devPub, _, _ := ed25519.GenerateKey(rand.Reader)
		devPubStr := base64.StdEncoding.EncodeToString(devPub)

		// Register with hub (now sending real pubkey so hub can verify signatures)
		regMsg := Message{
			Source:      "web-portal",
			Destination: "hub",
			Command:     "register",
			Payload: map[string]string{
				"version":    getBuildVersion(),
				"public_key": devPubStr,
			},
			Timestamp: time.Now().Format(time.RFC3339),
			Signature: "dummy", // still dummy for register in this dev build; hub allows under AEGIS_DEV_MODE
		}
		encoder := json.NewEncoder(conn)
		decoder := json.NewDecoder(conn)

		if err := encoder.Encode(regMsg); err != nil {
			conn.Close()
			continue
		}

		// Consume registration response
		var regResp map[string]interface{}
		if err := decoder.Decode(&regResp); err != nil {
			conn.Close()
			continue
		}
		if errMsg, ok := regResp["error"]; ok {
			log.Printf("Registration failed: %v", errMsg)
			conn.Close()
			continue
		}

		// Message handling loop
		for {
			var msg Message
			if err := decoder.Decode(&msg); err != nil {
				break
			}

			if msg.Command == "version" || msg.Command == "get-version" {
				response := Message{
					Source:      "web-portal",
					Destination: msg.Source,
					Command:     "version",
					Payload:     map[string]string{"version": getBuildVersion()},
					Timestamp:   time.Now().Format(time.RFC3339),
					Signature:   "dummy",
				}
				encoder.Encode(response)
			}
		}
		conn.Close()
	}
}

func newMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/chat/stream", handleChatStream)
	mux.HandleFunc("/api/dashboard", handleDashboard)
	mux.HandleFunc("/api/skills", handleSkills)
	mux.HandleFunc("/api/proposals", handleProposals)
	mux.HandleFunc("/api/monitoring", handleMonitoring)

	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(staticSub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the leading slash and clean the path before probing the embedded FS.

		// Go's embed FS already prevents path traversal, but we normalise explicitly.

		cleanPath := filepath.Clean(r.URL.Path[1:])
		_, fsErr := fs.Stat(staticSub, cleanPath)
		if fsErr != nil && r.URL.Path != "/" {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return securityMiddleware(mux)
}

func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		next.ServeHTTP(w, r)
	})
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Cache-Control", "no-store")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"service": "web-portal",
		"time":    time.Now().Format(time.RFC3339),
	})
}

func handleChatStream(w http.ResponseWriter, r *http.Request) {
	message := strings.TrimSpace(r.URL.Query().Get("message"))
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if message == "" || sessionID == "" {
		http.Error(w, "Missing message or session_id", http.StatusBadRequest)
		return
	}
	if len(message) > 2000 || !isSafeSessionID(sessionID) {
		http.Error(w, "Invalid message or session_id", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	traceID := nextID("trace")
	sendSSE(w, StreamMessage{
		Type:      "user_message",
		MessageID: nextID("msg"),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
		Content:   map[string]interface{}{"text": message},
		Metadata:  map[string]interface{}{"origin": "browser"},
	})
	flusher.Flush()

	time.Sleep(initialResponseDelay)
	sendSSE(w, StreamMessage{
		Type:      "agent_thinking",
		MessageID: nextID("msg"),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
		Content: map[string]interface{}{
			"step":        "observe_think_plan",
			"description": "Observe → Think → Plan",
		},
		Metadata: map[string]interface{}{"timing": "200ms"},
	})
	flusher.Flush()

	time.Sleep(thinkingDelay)
	sendSSE(w, StreamMessage{
		Type:      "tool_call",
		MessageID: nextID("msg"),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
		Content: map[string]interface{}{
			"tool": "ollama.generate",
			"args": map[string]string{"prompt": message},
		},
		Metadata: map[string]interface{}{"timing": "150ms"},
	})
	flusher.Flush()

	responseText, err := callOllama(fmt.Sprintf("User request: %s", message))
	time.Sleep(toolResultDelay)
	if err != nil {
		sendSSE(w, StreamMessage{
			Type:      "tool_result",
			MessageID: nextID("msg"),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   traceID,
			Content: map[string]interface{}{
				"tool":   "ollama.generate",
				"result": "error",
			},
			Metadata: map[string]interface{}{"timing": "200ms", "error": err.Error()},
		})
		sendSSE(w, StreamMessage{
			Type:      "error",
			MessageID: nextID("msg"),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   traceID,
			Content:   map[string]interface{}{"message": "Ollama backend unavailable"},
			Metadata:  map[string]interface{}{"detail": err.Error()},
		})
		responseText = "Ollama backend unavailable. Check Network Boundary/Ollama status and retry."
	} else {
		sendSSE(w, StreamMessage{
			Type:      "tool_result",
			MessageID: nextID("msg"),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   traceID,
			Content: map[string]interface{}{
				"tool":   "ollama.generate",
				"result": "ok",
			},
			Metadata: map[string]interface{}{"timing": "200ms"},
		})
	}
	flusher.Flush()

	time.Sleep(finalResponseDelay)
	chunks := incrementalChunks(responseText)
	lastChunkIndex := len(chunks) - 1
	for i, chunk := range chunks {
		sendSSE(w, StreamMessage{
			Type:      "agent_response",
			MessageID: nextID("msg"),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   traceID,
			Content: map[string]interface{}{
				"text":        chunk,
				"is_complete": i == lastChunkIndex,
			},
			Metadata: map[string]interface{}{},
		})
		flusher.Flush()
		time.Sleep(wordStreamDelay)
	}
}

func callOllama(prompt string) (string, error) {
	payload := ollamaGenerateRequest{Model: ollamaModel(), Prompt: prompt, Stream: false}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: ollamaRequestTimeout}
	resp, err := client.Post(ollamaEndpoint(), "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama request failed: %s", resp.Status)
	}

	var parsed ollamaGenerateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.Response) == "" {
		return "", fmt.Errorf("ollama returned empty response")
	}
	return strings.TrimSpace(parsed.Response), nil
}

func ollamaModel() string {
	if model := strings.TrimSpace(os.Getenv("AEGIS_OLLAMA_MODEL")); model != "" {
		return model
	}
	if model := strings.TrimSpace(os.Getenv("AEGIS_DEFAULT_MODEL")); model != "" {
		return model
	}
	return defaultOllamaModel
}

func ollamaEndpoint() string {
	if raw := strings.TrimSpace(os.Getenv("AEGIS_OLLAMA_URL")); raw != "" {
		if strings.Contains(raw, "/api/generate") || strings.Contains(raw, "/proxy/ollama/generate") {
			return raw
		}
		return strings.TrimRight(raw, "/") + "/api/generate"
	}
	return "http://localhost:8081/proxy/ollama/generate"
}

func incrementalChunks(text string) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	chunks := make([]string, 0, len(words))
	current := ""
	for i, word := range words {
		if i > 0 {
			current += " "
		}
		current += word
		chunks = append(chunks, current)
	}
	return chunks
}

func sendSSE(w http.ResponseWriter, msg StreamMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("failed to marshal SSE event %s: %v", msg.Type, err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	skills := loadSkills()
	proposals := loadProposals()
	status := loadDaemonStatus()
	logs := loadRecentDaemonLogs(3)
	pending := countPendingProposals(proposals)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"system_status": statusLabel(status.Running),
		"runtime":       status.Backend,
		"safe_mode":     status.SafeMode,
		"notifications": pending,
		"quick_stats": map[string]interface{}{
			"active_agents":     0,
			"background_tasks":  0,
			"skills_installed":  len(skills),
			"pending_proposals": pending,
		},
		"agents":          []map[string]interface{}{},
		"tasks":           []map[string]interface{}{},
		"recent_activity": logs,
	})
}

func handleSkills(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, loadSkills())
}

func handleProposals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, loadProposals())
}

func handleMonitoring(w http.ResponseWriter, r *http.Request) {
	status := loadDaemonStatus()
	logs := loadRecentDaemonLogs(50)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"safe_mode": status.SafeMode,
		"agents":    []map[string]interface{}{},
		"stats": map[string]interface{}{
			"running_vms":      status.RunningVMs,
			"background_tasks": 0,
			"cpu_usage":        "unknown",
			"memory_usage":     "unknown",
		},
		"logs": logs,
	})
}

func loadSkills() []map[string]interface{} {
	values := loadJSONObjects(storeSkillsFilename())
	skills := make([]map[string]interface{}, 0, len(values))
	for _, item := range values {
		skill := map[string]interface{}{}
		for k, v := range item {
			skill[k] = v
		}
		if _, ok := skill["id"]; !ok {
			skill["id"] = unknownStatus
		}
		if _, ok := skill["name"]; !ok {
			skill["name"] = fmt.Sprintf("%v", skill["id"])
		}
		if _, ok := skill["version"]; !ok {
			skill["version"] = "n/a"
		}
		if _, ok := skill["status"]; !ok {
			skill["status"] = "Unknown"
		}
		if _, ok := skill["description"]; !ok {
			skill["description"] = ""
		}
		if _, ok := skill["required_scopes"]; !ok {
			skill["required_scopes"] = []string{}
		}
		if _, ok := skill["secrets"]; !ok {
			skill["secrets"] = []string{}
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return fmt.Sprintf("%v", skills[i]["id"]) < fmt.Sprintf("%v", skills[j]["id"])
	})
	return skills
}

func loadProposals() []map[string]interface{} {
	values := loadJSONObjects(storeProposalsFilename())
	proposals := make([]map[string]interface{}, 0, len(values))
	for _, item := range values {
		proposal := map[string]interface{}{}
		for k, v := range item {
			proposal[k] = v
		}
		if _, ok := proposal["id"]; !ok {
			proposal["id"] = nextID("proposal")
		}
		title := fmt.Sprintf("%v", proposal["id"])
		if desc, ok := proposal["description"].(string); ok && strings.TrimSpace(desc) != "" {
			title = desc
		}
		proposal["title"] = title
		proposal["status"] = normalizeProposalStatus(fmt.Sprintf("%v", proposal["state"]))
		proposal["summary"] = fmt.Sprintf("Proposal %s", title)
		proposal["votes"] = summarizeVotes(proposal["reviews"])
		proposal["security_gates"] = []string{"Pending backend security gates"}
		proposals = append(proposals, proposal)
	}
	sort.Slice(proposals, func(i, j int) bool {
		return fmt.Sprintf("%v", proposals[i]["id"]) < fmt.Sprintf("%v", proposals[j]["id"])
	})
	return proposals
}

func summarizeVotes(raw interface{}) string {
	votes, ok := raw.(map[string]interface{})
	if !ok || len(votes) == 0 {
		return "No votes"
	}
	approve := 0
	reject := 0
	abstain := 0
	for _, v := range votes {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v)))
		switch s {
		case "approve":
			approve++
		case "reject":
			reject++
		case "abstain":
			abstain++
		}
	}
	return fmt.Sprintf("%d approve / %d reject / %d abstain", approve, reject, abstain)
}

func normalizeProposalStatus(state string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	switch s {
	case "approved":
		return "APPROVED"
	case "rejected":
		return "REJECTED"
	case "pending", "under_review", "under review":
		return "UNDER REVIEW"
	default:
		return "UNDER REVIEW"
	}
}

func countPendingProposals(proposals []map[string]interface{}) int {
	pending := 0
	for _, proposal := range proposals {
		status := strings.ToLower(fmt.Sprintf("%v", proposal["status"]))
		if strings.Contains(status, "review") || strings.Contains(status, "pending") {
			pending++
		}
	}
	return pending
}

func loadJSONObjects(filename string) []map[string]interface{} {
	path := filepath.Join(storeDataDir(), filename)
	raw, err := os.ReadFile(path)
	if err != nil {
		return []map[string]interface{}{}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return []map[string]interface{}{}
	}

	asMap := map[string]interface{}{}
	if err := json.Unmarshal(raw, &asMap); err == nil {
		list := make([]map[string]interface{}, 0, len(asMap))
		for key, value := range asMap {
			if obj, ok := value.(map[string]interface{}); ok {
				if _, exists := obj["id"]; !exists {
					obj["id"] = key
				}
				list = append(list, obj)
			}
		}
		return list
	}

	asList := []map[string]interface{}{}
	if err := json.Unmarshal(raw, &asList); err == nil {
		return asList
	}

	return []map[string]interface{}{}
}

func storeDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("AEGIS_STORE_DATA_DIR")); dir != "" {
		return dir
	}
	return "."
}

func storeSkillsFilename() string {
	if file := strings.TrimSpace(os.Getenv("AEGIS_SKILLS_FILE")); file != "" {
		return file
	}
	return "skills.json"
}

func storeProposalsFilename() string {
	if file := strings.TrimSpace(os.Getenv("AEGIS_PROPOSALS_FILE")); file != "" {
		return file
	}
	return "proposals.json"
}

func loadDaemonStatus() daemonSnapshot {
	snapshot := daemonSnapshot{
		Running:    false,
		Backend:    unknownStatus,
		RunningVMs: 0,
		Hub:        unknownStatus,
		MemoryVM:   unknownStatus,
		StoreVM:    unknownStatus,
	}
	socket := expandPath("~/.aegis/daemon.sock")
	conn, err := net.DialTimeout("unix", socket, daemonConnectTimeout)
	if err != nil {
		return snapshot
	}
	defer conn.Close()

	snapshot.Running = true
	_ = conn.SetDeadline(time.Now().Add(daemonReadDeadline))
	if _, err := conn.Write([]byte("status")); err != nil {
		return snapshot
	}

	resp, err := io.ReadAll(conn)
	if err != nil {
		return snapshot
	}
	parseDaemonStatus(&snapshot, string(resp))
	return snapshot
}

func parseDaemonStatus(snapshot *daemonSnapshot, raw string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "Backend":
			snapshot.Backend = value
		case "Safe Mode":
			snapshot.SafeMode = strings.EqualFold(value, "true")
		case "Running VMs":
			if n, err := strconv.Atoi(value); err == nil {
				snapshot.RunningVMs = n
			}
		case "Hub":
			snapshot.Hub = value
		case "Memory VM":
			snapshot.MemoryVM = value
		case "Store VM":
			snapshot.StoreVM = value
		}
	}
}

func loadRecentDaemonLogs(limit int) []string {
	if limit <= 0 {
		return []string{}
	}
	path := expandPath("~/.aegis/daemon.log")
	file, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer file.Close()

	lines := make([]string, 0, limit)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func statusLabel(running bool) string {
	if running {
		return "running"
	}
	return "degraded"
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func nextID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
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

func isSafeSessionID(sessionID string) bool {
	return len(sessionID) > 0 && len(sessionID) <= 128 && sessionIDPattern.MatchString(sessionID)
}

func runWebPortal(cmd *cobra.Command, args []string) {
	// Start message handler for hub communication
	go handleHubMessages()

	fmt.Println("Web Portal starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", newMux()))
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "web-portal",
		Short: "Web Portal",
		Run:   runWebPortal,
	}

	rootCmd.Execute()
}
