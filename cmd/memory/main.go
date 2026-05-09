package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

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

func runMemory(cmd *cobra.Command, args []string) {
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
		Source:      "memory",
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   "2026-05-09T19:35:00Z",
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
	fmt.Println("Memory VM registered")

	// In-memory store for basics
	memories := make(map[string]interface{})
	var mu sync.Mutex

	// Memory loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Memory received:", msg.Command)

		response := Message{
			Source:      "memory",
			Destination: msg.Source,
			Timestamp:   "2026-05-09T19:35:01Z",
			Signature:   "dummy",
		}

		mu.Lock()
		switch msg.Command {
		case "memory.get_context":
			response.Command = "memory.context"
			response.Payload = map[string]interface{}{
				"short_term": []string{"recent message"},
				"long_term":  memories,
			}
		case "memory.store":
			payload := msg.Payload.(map[string]interface{})
			key := payload["content"].(string)
			memories[key] = payload
			response.Command = "memory.stored"
			response.Payload = "ok"
		case "memory.search":
			payload := msg.Payload.(map[string]interface{})
			query := payload["query"].(string)
			results := []interface{}{}
			for k, v := range memories {
				if k == query { // simple match
					results = append(results, v)
				}
			}
			response.Command = "memory.results"
			response.Payload = results
		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}
		mu.Unlock()

		err = encoder.Encode(response)
		if err != nil {
			log.Println("Failed to send response:", err)
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "memory",
		Short: "Memory VM",
		Run:   runMemory,
	}

	rootCmd.Execute()
}
