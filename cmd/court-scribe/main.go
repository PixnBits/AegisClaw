package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
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

func init() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocket = env
	}
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

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func runCourtScribe(cmd *cobra.Command, args []string) {
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
		Source:      "court-scribe",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"version": getBuildVersion(),
		},
		Timestamp: "2026-05-09T19:45:00Z",
		Signature: "dummy",
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
	fmt.Println("Court Scribe registered")

	// Review states
	reviews := make(map[string]map[string]string) // proposal_id -> persona -> vote
	var mu sync.Mutex

	// Scribe loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Scribe received:", msg.Command)

		response := Message{
			Source:      "court-scribe",
			Destination: msg.Source,
			Timestamp:   "2026-05-09T19:45:01Z",
			Signature:   "dummy",
		}

		mu.Lock()
		switch msg.Command {
		case "scribe.notify_review":
			payload := msg.Payload.(map[string]interface{})
			proposalID := payload["proposal_id"].(string)
			// Notify personas (simulate)
			fmt.Println("Notifying court personas for proposal", proposalID)
			reviews[proposalID] = make(map[string]string)
			response.Command = "scribe.notified"
			response.Payload = "ok"
		case "scribe.submit_vote":
			payload := msg.Payload.(map[string]interface{})
			proposalID := payload["proposal_id"].(string)
			persona := payload["persona"].(string)
			vote := payload["vote"].(string)
			if reviews[proposalID] != nil {
				reviews[proposalID][persona] = vote
				// Check if all voted (simulate 7 personas)
				if len(reviews[proposalID]) >= 7 {
					response.Command = "scribe.review_complete"
					response.Payload = map[string]interface{}{
						"proposal_id": proposalID,
						"votes":       reviews[proposalID],
						"approved":    true, // simulate
					}
					// Notify store
					storeMsg := Message{
						Source:      "court-scribe",
						Destination: "store",
						Command:     "court.review_complete",
						Payload:     response.Payload,
						Timestamp:   response.Timestamp,
						Signature:   "dummy",
					}
					encoder.Encode(storeMsg)
				} else {
					response.Command = "scribe.vote_recorded"
					response.Payload = "ok"
				}
			}
		case "scribe.get_review_status":
			payload := msg.Payload.(map[string]interface{})
			proposalID := payload["proposal_id"].(string)
			response.Command = "scribe.status"
			response.Payload = reviews[proposalID]
		case "version", "get-version":
			if msg.Command == "get-version" {
				// For get-version from hub, send proper Message response back
				response.Command = "version"
				response.Source = "court-scribe"
				response.Destination = msg.Source
				response.Payload = map[string]string{"version": getBuildVersion()}
				// Don't continue - let normal flow sign and send
			} else {
				response.Command = "version"
				response.Payload = map[string]string{"version": getBuildVersion()}
			}
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
		Use:   "court-scribe",
		Short: "Court Scribe",
		Run:   runCourtScribe,
	}

	rootCmd.Execute()
}
