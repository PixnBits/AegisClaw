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
	"sync"
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

const (
	numPersonas = 7
)

var courtPersonas = []string{
	"ciso",
	"security-architect",
	"architect",
	"senior-coder",
	"tester",
	"efficiency",
	"user-advocate",
}

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

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

// decideReview implements governance-court.md rules:
// - Any Reject -> blocked (approved=false)
// - Approved only on unanimous Approve from all non-abstaining personas
// - Abstain ok (common for high-level); must have at least one non-abstain Approve if no rejects
func decideReview(votes map[string]string) bool {
	rejects := 0
	approves := 0
	abstains := 0
	for _, v := range votes {
		switch v {
		case "Reject":
			rejects++
		case "Approve":
			approves++
		case "Abstain":
			abstains++
		}
	}
	if rejects > 0 {
		return false
	}
	nonAbstainers := approves + rejects // rejects==0 here
	if nonAbstainers == 0 {
		return false // all abstained, cannot approve
	}
	return approves == nonAbstainers // all non-abstainers approved (unanimous)
}

func runCourtScribe(cmd *cobra.Command, args []string) {
	// Generate keys for signing (required for strict Hub ACL + sig verify)
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
	var connMutex sync.Mutex

	// Register with pubkey (Phase 3: all components must for sig verification)
	regMsg := Message{
		Source:      "court-scribe",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Signature: "dummy", // will be overwritten; hub allows in DEV
	}
	signMessage(&regMsg, priv) // sign even reg for consistency
	connMutex.Lock()
	err = encoder.Encode(regMsg)
	connMutex.Unlock()
	if err != nil {
		log.Fatal("Failed to register:", err)
	}

	// Consume response
	var resp map[string]interface{}
	connMutex.Lock()
	err = decoder.Decode(&resp)
	connMutex.Unlock()
	if err != nil {
		log.Fatal("Failed to decode register response:", err)
	}
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	fmt.Println("Court Scribe registered (with pubkey + signing)")

	// Review states
	reviews := make(map[string]map[string]string) // proposal_id -> persona -> vote
	var mu sync.Mutex

	// Scribe loop
	for {
		var msg Message
		connMutex.Lock()
		err := decoder.Decode(&msg)
		connMutex.Unlock()
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
			payload, _ := msg.Payload.(map[string]interface{})
			proposalID, _ := payload["proposal_id"].(string)
			// Security: Scribe must never receive or store proposal content (per spec)
			if _, hasContent := payload["description"]; hasContent || payload["extracted"] != nil {
				log.Printf("Audit: Court Scribe received content in notify for %s - rejecting", proposalID)
				response.Command = "error"
				response.Payload = "ERR_SCRIBE_NO_CONTENT"
				break
			}
			if reviews[proposalID] == nil {
				reviews[proposalID] = make(map[string]string)
				fmt.Println("Scribe: tracking new review for", proposalID)
				// Forward notify to all 7 distinct personas via Hub (now that ACL + wildcard supported)
				for _, p := range courtPersonas {
					personaDest := "court-persona-" + p
					notify := Message{
						Source:      "court-scribe",
						Destination: personaDest,
						Command:     "scribe.notify_review",
						Payload:     map[string]interface{}{"proposal_id": proposalID},
						Timestamp:   time.Now().Format(time.RFC3339),
						Signature:   "",
					}
					signMessage(&notify, priv)
					connMutex.Lock()
					encoder.Encode(notify)
					connMutex.Unlock()
				}
				fmt.Println("Scribe: forwarded notify_review to", len(courtPersonas), "personas for", proposalID)
			}
			response.Command = "scribe.notified"
			response.Payload = "ok"
		case "scribe.submit_vote":
			payload, _ := msg.Payload.(map[string]interface{})
			proposalID, _ := payload["proposal_id"].(string)
			persona, _ := payload["persona"].(string)
			vote, _ := payload["vote"].(string)
			if reviews[proposalID] != nil {
				reviews[proposalID][persona] = vote
				fmt.Printf("Scribe: recorded vote %s from %s for %s\n", vote, persona, proposalID)
				if len(reviews[proposalID]) >= numPersonas {
					// Enforce voting rules (governance-court.md): unanimous Approve from non-abstainers; any Reject blocks
					approved := decideReview(reviews[proposalID])
					result := map[string]interface{}{
						"proposal_id": proposalID,
						"votes":       reviews[proposalID],
						"approved":    approved,
						"num_votes":   len(reviews[proposalID]),
					}
					response.Command = "scribe.review_complete"
					response.Payload = result
					// Signed notify to Store (and implicitly proposer via other flows)
					storeMsg := Message{
						Source:      "court-scribe",
						Destination: "store",
						Command:     "court.review_complete",
						Payload:     result,
						Timestamp:   time.Now().Format(time.RFC3339),
						Signature:   "",
					}
					signMessage(&storeMsg, priv)
					connMutex.Lock()
					encoder.Encode(storeMsg)
					connMutex.Unlock()
					fmt.Println("Scribe: review complete for", proposalID, "approved=", approved)
				} else {
					response.Command = "scribe.vote_recorded"
					response.Payload = "ok"
				}
			}
		case "scribe.get_review_status":
			payload, _ := msg.Payload.(map[string]interface{})
			proposalID, _ := payload["proposal_id"].(string)
			response.Command = "scribe.status"
			response.Payload = reviews[proposalID]
		case "version", "get-version":
			if msg.Command == "get-version" {
				response.Command = "version"
				response.Source = "court-scribe"
				response.Destination = msg.Source
				response.Payload = map[string]string{"version": getBuildVersion()}
			} else {
				response.Command = "version"
				response.Payload = map[string]string{"version": getBuildVersion()}
			}
		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}
		mu.Unlock()

		// Always sign responses (strict hub)
		signMessage(&response, priv)
		connMutex.Lock()
		err = encoder.Encode(response)
		connMutex.Unlock()
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
