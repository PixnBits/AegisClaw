package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
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

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func connectToHub() (net.Conn, error) {
	socket := expandPath(hubSocket)
	return net.Dial("unix", socket)
}

func handleChatStream(w http.ResponseWriter, r *http.Request) {
	message := r.URL.Query().Get("message")
	sessionID := r.URL.Query().Get("session_id")
	if message == "" || sessionID == "" {
		http.Error(w, "Missing message or session_id", 400)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	// Send user_message first
	userMsg := StreamMessage{
		Type:      "user_message",
		MessageID: "msg_" + fmt.Sprintf("%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   "trace_" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Content:   map[string]interface{}{"text": message},
		Metadata:  map[string]interface{}{},
	}
	sendSSE(w, userMsg)
	flusher.Flush()

	// Simulate agent response with RAIL principle
	go func() {
		time.Sleep(200 * time.Millisecond) // Fast first feedback < 300ms

		// agent_thinking
		thinkingMsg := StreamMessage{
			Type:      "agent_thinking",
			MessageID: "msg_" + fmt.Sprintf("%d", time.Now().UnixNano()),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   "trace_" + fmt.Sprintf("%d", time.Now().UnixNano()),
			Content:   map[string]interface{}{"step": "analyze_request", "description": "Understanding user request"},
			Metadata:  map[string]interface{}{},
		}
		sendSSE(w, thinkingMsg)
		flusher.Flush()

		time.Sleep(300 * time.Millisecond)

		// tool_call
		toolMsg := StreamMessage{
			Type:      "tool_call",
			MessageID: "msg_" + fmt.Sprintf("%d", time.Now().UnixNano()),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   "trace_" + fmt.Sprintf("%d", time.Now().UnixNano()),
			Content:   map[string]interface{}{"tool": "web_search", "args": map[string]string{"query": message}},
			Metadata:  map[string]interface{}{"timing": "150ms"},
		}
		sendSSE(w, toolMsg)
		flusher.Flush()

		time.Sleep(200 * time.Millisecond)

		// tool_result
		resultMsg := StreamMessage{
			Type:      "tool_result",
			MessageID: "msg_" + fmt.Sprintf("%d", time.Now().UnixNano()),
			SessionID: sessionID,
			Timestamp: time.Now().Format(time.RFC3339),
			TraceID:   "trace_" + fmt.Sprintf("%d", time.Now().UnixNano()),
			Content:   map[string]interface{}{"tool": "web_search", "result": "Found relevant information"},
			Metadata:  map[string]interface{}{"timing": "200ms"},
		}
		sendSSE(w, resultMsg)
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		// Incremental agent_response
		responseText := "Based on my analysis, here's what I found:\n\n"
		words := []string{"The", "user", "asked", "about", message, "and", "I", "have", "some", "insights."}

		for i, word := range words {
			responseText += word + " "
			respMsg := StreamMessage{
				Type:      "agent_response",
				MessageID: "msg_" + fmt.Sprintf("%d", time.Now().UnixNano()),
				SessionID: sessionID,
				Timestamp: time.Now().Format(time.RFC3339),
				TraceID:   "trace_" + fmt.Sprintf("%d", time.Now().UnixNano()),
				Content:   map[string]interface{}{"text": responseText, "is_complete": i == len(words)-1},
				Metadata:  map[string]interface{}{},
			}
			sendSSE(w, respMsg)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

func sendSSE(w http.ResponseWriter, msg StreamMessage) {
	data, _ := json.Marshal(msg)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func runWebPortal(cmd *cobra.Command, args []string) {
	// API routes
	http.HandleFunc("/api/chat/stream", handleChatStream)
	http.HandleFunc("/api/dashboard", handleDashboard)
	http.HandleFunc("/api/skills", handleSkills)
	http.HandleFunc("/api/proposals", handleProposals)

	// Static files
	http.Handle("/", http.FileServer(http.Dir("./static")))

	fmt.Println("Web Portal starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Mock dashboard data
	data := map[string]interface{}{
		"status": "running",
		"agents": 1,
		"skills": 5,
		"proposals": 2,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func handleSkills(w http.ResponseWriter, r *http.Request) {
	// Mock skills data
	data := []map[string]interface{}{
		{"id": "discord_monitor", "name": "Discord Monitor", "description": "Monitors Discord for keywords"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func handleProposals(w http.ResponseWriter, r *http.Request) {
	// Mock proposals data
	data := []map[string]interface{}{
		{"id": "prop1", "description": "Add new feature", "status": "pending"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "web-portal",
		Short: "Web Portal",
		Run:   runWebPortal,
	}

	rootCmd.Execute()
}
