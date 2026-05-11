package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
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

var hubSocket = "~/.aegis/hub.sock"

var (
	initialResponseDelay = 200 * time.Millisecond
	thinkingDelay        = 300 * time.Millisecond
	toolResultDelay      = 200 * time.Millisecond
	finalResponseDelay   = 100 * time.Millisecond
	wordStreamDelay      = 50 * time.Millisecond
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
	mux.Handle("/", http.FileServer(http.FS(staticSub)))

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
	userMsg := StreamMessage{
		Type:      "user_message",
		MessageID: nextID("msg"),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
		Content:   map[string]interface{}{"text": message},
		Metadata:  map[string]interface{}{"origin": "browser"},
	}
	sendSSE(w, userMsg)
	flusher.Flush()

	time.Sleep(initialResponseDelay)
	thinkingMsg := StreamMessage{
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
	}
	sendSSE(w, thinkingMsg)
	flusher.Flush()

	time.Sleep(thinkingDelay)
	toolMsg := StreamMessage{
		Type:      "tool_call",
		MessageID: nextID("msg"),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
		Content: map[string]interface{}{
			"tool": "tool.search",
			"args": map[string]string{"query": message},
		},
		Metadata: map[string]interface{}{"timing": "150ms"},
	}
	sendSSE(w, toolMsg)
	flusher.Flush()

	time.Sleep(toolResultDelay)
	resultMsg := StreamMessage{
		Type:      "tool_result",
		MessageID: nextID("msg"),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
		Content: map[string]interface{}{
			"tool":   "tool.search",
			"result": "Matched secure internal guidance and current portal context.",
		},
		Metadata: map[string]interface{}{"timing": "200ms"},
	}
	sendSSE(w, resultMsg)
	flusher.Flush()

	time.Sleep(finalResponseDelay)
	responseText := fmt.Sprintf("## Assessment\n- Request: %s\n- Portal posture: self-contained and security-first\n\nAegisClaw keeps the user informed with transparent tool activity, stable controls, and no external frontend dependencies.", message)
	chunks := incrementalChunks(responseText)
	for i, chunk := range chunks {
		respMsg := StreamMessage{
			Type:      "agent_response",
			MessageID: nextID("msg"),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   traceID,
			Content: map[string]interface{}{
				"text":        chunk,
				"is_complete": i == len(chunks)-1,
			},
			Metadata: map[string]interface{}{},
		}
		sendSSE(w, respMsg)
		flusher.Flush()
		time.Sleep(wordStreamDelay)
	}
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
	data, _ := json.Marshal(msg)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"system_status": "running",
		"runtime":       "Firecracker",
		"safe_mode":     false,
		"notifications": 2,
		"quick_stats": map[string]interface{}{
			"active_agents":     3,
			"background_tasks":  2,
			"skills_installed":  24,
			"pending_proposals": 2,
		},
		"agents": []map[string]interface{}{
			{"name": "researcher", "status": "working", "task": "Reviewing Zig adoption", "progress": "4m"},
			{"name": "analyst", "status": "idle", "task": "Awaiting direction", "progress": "0m"},
			{"name": "general", "status": "working", "task": "Monitoring email summaries", "progress": "2m"},
		},
		"tasks": []map[string]interface{}{
			{"name": "Research Zig adoption", "status": "running", "progress": "12% complete"},
			{"name": "Daily security scan", "status": "running", "progress": "running"},
		},
		"recent_activity": []string{
			"discord_monitor deployed v1.2",
			"Court approved web_search v2",
			"Audit log verification completed",
		},
	})
}

func handleSkills(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]interface{}{
		{
			"id":              "discord_monitor",
			"name":            "Discord Monitor",
			"version":         "1.2",
			"status":          "Deployed",
			"description":     "Monitor Discord servers for keywords and send summaries.",
			"required_scopes": []string{"network:discord.com", "background"},
			"secrets":         []string{"DISCORD_BOT_TOKEN"},
		},
		{
			"id":              "web_search",
			"name":            "Web Search",
			"version":         "2.1",
			"status":          "Deployed",
			"description":     "Search curated web sources through approved boundaries.",
			"required_scopes": []string{"network:approved-search"},
			"secrets":         []string{},
		},
		{
			"id":              "email_client",
			"name":            "Email Client",
			"version":         "0.9",
			"status":          "Building",
			"description":     "Draft and summarize email workflows through Court-reviewed automation.",
			"required_scopes": []string{"network:mail-relay"},
			"secrets":         []string{"SMTP_TOKEN"},
		},
	})
}

func handleProposals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]interface{}{
		{
			"id":             "discord_monitor_v1_2",
			"title":          "discord_monitor v1.2",
			"status":         "APPROVED",
			"summary":        "Security gates passed and deployment completed.",
			"votes":          "7/7 unanimous",
			"security_gates": []string{"SAST Passed", "SCA Passed", "Secrets Scan Passed"},
		},
		{
			"id":             "web_search_v2",
			"title":          "web_search v2",
			"status":         "UNDER REVIEW",
			"summary":        "Awaiting additional Court votes.",
			"votes":          "4 approve / 2 reject / 1 abstain",
			"security_gates": []string{"Build Ready", "SBOM Available"},
		},
	})
}

func handleMonitoring(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"safe_mode": false,
		"agents": []map[string]interface{}{
			{"name": "researcher", "status": "Working", "progress": "87%"},
			{"name": "analyst", "status": "Idle", "progress": "0%"},
			{"name": "general", "status": "Working", "progress": "42%"},
		},
		"stats": map[string]interface{}{
			"running_vms":      4,
			"background_tasks": 7,
			"cpu_usage":        "34%",
			"memory_usage":     "18GB",
		},
		"logs": []string{
			"14:32:11 researcher: Found 12 relevant papers",
			"14:32:09 analyst: Key tradeoff identified",
			"14:31:55 general: New email batch processed",
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func nextID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func isSafeSessionID(sessionID string) bool {
	if len(sessionID) == 0 || len(sessionID) > 128 {
		return false
	}
	for _, r := range sessionID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func runWebPortal(cmd *cobra.Command, args []string) {
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
