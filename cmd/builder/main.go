package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
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

func runBuilder(cmd *cobra.Command, args []string) {
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
		Source:      "builder",
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   "2026-05-09T20:00:00Z",
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
	fmt.Println("Builder VM registered")

	// Builder loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Builder received:", msg.Command)

		response := Message{
			Source:      "builder",
			Destination: msg.Source,
			Timestamp:   "2026-05-09T20:00:01Z",
			Signature:   "dummy",
		}

		switch msg.Command {
		case "store.git.clone":
			// Stub: simulate cloning
			response.Command = "git.cloned"
			response.Payload = "ok"
		case "store.git.push":
			response.Command = "git.pushed"
			response.Payload = "ok"
		case "store.pr.create":
			response.Command = "pr.created"
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
		Use:   "builder",
		Short: "Builder VM",
		Run:   runBuilder,
	}

	rootCmd.Execute()
}