package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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

func loadFromFile(filename string) map[string]interface{} {
	data := make(map[string]interface{})
	file, err := os.Open(filename)
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
}

func saveToFile(filename string, data interface{}) {
	bytes, _ := json.Marshal(data)
	ioutil.WriteFile(filename, bytes, 0644)
}

func loadAuditFromFile(filename string) []interface{} {
	var data []interface{}
	file, err := os.Open(filename)
	if err != nil {
		return data
	}
	defer file.Close()
	json.NewDecoder(file).Decode(&data)
	return data
}

func saveAuditToFile(filename string, data []interface{}) {
	bytes, _ := json.Marshal(data)
	ioutil.WriteFile(filename, bytes, 0644)
}

func runStore(cmd *cobra.Command, args []string) {
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
		Source:      "store",
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   "2026-05-09T19:40:00Z",
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
	fmt.Println("Store VM registered")

	// Simple storage with persistence
	proposals := loadFromFile("proposals.json")
	skills := loadFromFile("skills.json")
	auditLog := loadAuditFromFile("audit.json")
	var mu sync.Mutex

	// Store loop
	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("Decode error:", err)
			continue
		}

		fmt.Println("Store received:", msg.Command)

		response := Message{
			Source:      "store",
			Destination: msg.Source,
			Timestamp:   "2026-05-09T19:40:01Z",
			Signature:   "dummy",
		}

		mu.Lock()
		switch msg.Command {
		case "proposal.create":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			proposals[id] = payload
			saveToFile("proposals.json", proposals)
			// Notify scribe
			scribeMsg := Message{
				Source:      "store",
				Destination: "court-scribe",
				Command:     "scribe.notify_review",
				Payload:     map[string]interface{}{"proposal_id": id},
				Timestamp:   response.Timestamp,
				Signature:   "dummy",
			}
			encoder.Encode(scribeMsg)
			response.Command = "proposal.created"
			response.Payload = "ok"
		case "proposal.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "proposal.data"
			response.Payload = proposals[id]
		case "skill.register":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			skills[id] = payload
			saveToFile("skills.json", skills)
			response.Command = "skill.registered"
			response.Payload = "ok"
		case "audit.append":
			auditLog = append(auditLog, msg.Payload)
			saveAuditToFile("audit.json", auditLog)
			response.Command = "audit.appended"
			response.Payload = "ok"
		case "tool.list":
			response.Command = "tool.list"
			response.Payload = skills
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
		Use:   "store",
		Short: "Store VM",
		Run:   runStore,
	}

	rootCmd.Execute()
}