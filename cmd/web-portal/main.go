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

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// Connect to hub
	conn, err := connectToHub()
	if err != nil {
		http.Error(w, "Failed to connect to hub", 500)
		return
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)

	// Send message to agent
	msg := Message{
		Source:      "web-portal",
		Destination: "agent1",
		Command:     "user_message",
		Payload:     req.Message,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	err = encoder.Encode(msg)
	if err != nil {
		http.Error(w, "Failed to send message", 500)
		return
	}

	// Stream response
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	// Simulate streaming
	response := "Agent thinking...\n"
	fmt.Fprint(w, response)
	flusher.Flush()
	time.Sleep(100 * time.Millisecond)

	response = "Tool call: search\n"
	fmt.Fprint(w, response)
	flusher.Flush()
	time.Sleep(100 * time.Millisecond)

	response = "Response: " + req.Message
	fmt.Fprint(w, response)
	flusher.Flush()
}

func runWebPortal(cmd *cobra.Command, args []string) {
	http.HandleFunc("/chat", handleChat)
	http.Handle("/", http.FileServer(http.Dir("./static"))) // Assume static/chat.html

	fmt.Println("Web Portal starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "web-portal",
		Short: "Web Portal",
		Run:   runWebPortal,
	}

	rootCmd.Execute()
}
