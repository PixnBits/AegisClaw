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
var persona string

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func runCourtPersona(cmd *cobra.Command, args []string) {
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
		Source:      "court-" + persona,
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   "2026-05-09T19:50:00Z",
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
	fmt.Println("Court Persona", persona, "registered")

	// Persona loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Persona", persona, "received:", msg.Command)

		if msg.Command == "scribe.notify_review" {
			// Submit vote
			payload := msg.Payload.(map[string]interface{})
			proposalID := payload["proposal_id"].(string)
			voteMsg := Message{
				Source:      "court-" + persona,
				Destination: "court-scribe",
				Command:     "scribe.submit_vote",
				Payload: map[string]interface{}{
					"proposal_id": proposalID,
					"persona":     persona,
					"vote":        "Approve", // simulate
					"reasoning":   "Looks good from " + persona + " perspective",
				},
				Timestamp: "2026-05-09T19:50:01Z",
				Signature: "dummy",
			}
			err = encoder.Encode(voteMsg)
			if err != nil {
				log.Println("Failed to submit vote:", err)
			}
		}
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "court-persona",
		Short: "Court Persona",
		Run:   runCourtPersona,
	}

	rootCmd.Flags().StringVar(&persona, "persona", "", "Persona name")
	rootCmd.MarkFlagRequired("persona")

	rootCmd.Execute()
}