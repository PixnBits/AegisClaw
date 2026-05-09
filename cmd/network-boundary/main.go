package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

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

func runNetworkBoundary(cmd *cobra.Command, args []string) {
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
		Source:      "network-boundary",
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   "2026-05-09T20:05:00Z",
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
	fmt.Println("Network Boundary registered")

	// Start HTTP proxy
	go func() {
		http.HandleFunc("/proxy", func(w http.ResponseWriter, r *http.Request) {
			// Stub: allow example.com
			url := r.URL.Query().Get("url")
			if url == "http://example.com" {
				w.Write([]byte("Proxied response"))
			} else {
				http.Error(w, "Blocked", 403)
			}
		})
		log.Fatal(http.ListenAndServe(":8081", nil))
	}()

	// Boundary loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Network Boundary received:", msg.Command)

		response := Message{
			Source:      "network-boundary",
			Destination: msg.Source,
			Timestamp:   "2026-05-09T20:05:01Z",
			Signature:   "dummy",
		}

		switch msg.Command {
		case "network.request":
			// Stub proxy
			response.Command = "network.response"
			response.Payload = "ok"
		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}

		err = encoder.Encode(response)
		if err != nil {
			log.Println("Failed to send response:", err)
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "network-boundary",
		Short: "Network Boundary VM",
		Run:   runNetworkBoundary,
	}

	rootCmd.Execute()
}