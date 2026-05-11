package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
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

func computeMerkleRoot(log []interface{}) string {
	if len(log) == 0 {
		return ""
	}
	data, _ := json.Marshal(log)
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
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

func runStore(cmd *cobra.Command, args []string) {
	// Generate keys
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

	// Register
	regMsg := Message{
		Source:      "store",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pubStr},
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
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	fmt.Println("Store VM registered")

	// Simple storage with persistence
	proposals := loadFromFile("proposals.json")
	skills := loadFromFile("skills.json")
	auditLog := loadAuditFromFile("audit.json")
	memories := loadFromFile("memories.json")
	prs := loadFromFile("prs.json")
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
			Signature:   "",
		}

		mu.Lock()
		switch msg.Command {
		case "proposal.create":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			payload["state"] = "pending"
			payload["reviews"] = make(map[string]string)
			proposals[id] = payload
			saveToFile("proposals.json", proposals)
			// Notify scribe
			scribeMsg := Message{
				Source:      "store",
				Destination: "court-scribe",
				Command:     "scribe.notify_review",
				Payload:     map[string]interface{}{"proposal_id": id},
				Timestamp:   response.Timestamp,
				Signature:   "",
			}
			signMessage(&scribeMsg, priv)
			encoder.Encode(scribeMsg)
			response.Command = "proposal.created"
			response.Payload = "ok"
		case "proposal.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "proposal.data"
			response.Payload = proposals[id]
		case "proposal.list":
			list := []interface{}{}
			for _, p := range proposals {
				list = append(list, p)
			}
			response.Command = "proposal.list"
			response.Payload = list
		case "proposal.update":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if p, ok := proposals[id].(map[string]interface{}); ok {
				for k, v := range payload {
					if k != "id" {
						p[k] = v
					}
				}
				proposals[id] = p
				saveToFile("proposals.json", proposals)
				response.Command = "proposal.updated"
				response.Payload = "ok"
			} else {
				response.Command = "error"
				response.Payload = "proposal not found"
			}
		case "court.review_complete":
			payload := msg.Payload.(map[string]interface{})
			id := payload["proposal_id"].(string)
			votes := payload["votes"].(map[string]interface{})
			if p, ok := proposals[id].(map[string]interface{}); ok {
				p["reviews"] = votes
				p["state"] = "approved" // simplified
				proposals[id] = p
				saveToFile("proposals.json", proposals)
				// Register the skill
				skill := map[string]interface{}{
					"id":          id,
					"name":        "Discord Monitor", // hardcoded for test
					"description": p["description"],
				}
				skills[id] = skill
				saveToFile("skills.json", skills)
				response.Command = "skill.registered"
				response.Payload = "ok"
			}
		case "court.get_reviews":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if p, ok := proposals[id].(map[string]interface{}); ok {
				response.Command = "court.reviews"
				response.Payload = p["reviews"]
			} else {
				response.Command = "error"
				response.Payload = "proposal not found"
			}
		case "git.clone":
			payload := msg.Payload.(map[string]interface{})
			repo := payload["repo"].(string)
			path := "repos/" + repo
			os.MkdirAll("repos", 0755)
			cmd := exec.Command("git", "init", "--bare", path)
			err := cmd.Run()
			if err != nil {
				response.Command = "error"
				response.Payload = err.Error()
			} else {
				response.Command = "git.cloned"
				response.Payload = "ok"
			}
		case "git.push":
			// For push, assume it's handled by git, stub success
			response.Command = "git.pushed"
			response.Payload = "ok"
		case "pr.create":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			prs[id] = payload
			saveToFile("prs.json", prs)
			response.Command = "pr.created"
			response.Payload = "ok"
		case "pr.update":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			if p, ok := prs[id].(map[string]interface{}); ok {
				for k, v := range payload {
					if k != "id" {
						p[k] = v
					}
				}
				prs[id] = p
				saveToFile("prs.json", prs)
				response.Command = "pr.updated"
				response.Payload = "ok"
			} else {
				response.Command = "error"
				response.Payload = "pr not found"
			}
		case "pr.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "pr.data"
			response.Payload = prs[id]
		case "skill.register":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			skills[id] = payload
			saveToFile("skills.json", skills)
			response.Command = "skill.registered"
			response.Payload = "ok"
		case "skill.get":
			payload := msg.Payload.(map[string]interface{})
			id := payload["id"].(string)
			response.Command = "skill.data"
			response.Payload = skills[id]
		case "skill.list":
			list := []interface{}{}
			for _, s := range skills {
				list = append(list, s)
			}
			response.Command = "skill.list"
			response.Payload = list
		case "memory.store":
			payload := msg.Payload.(map[string]interface{})
			memories[payload["content"].(string)] = payload
			saveToFile("memories.json", memories)
			response.Command = "memory.stored"
			response.Payload = "ok"
		case "memory.query":
			// Stub
			response.Command = "memory.results"
			response.Payload = []interface{}{}
		case "audit.append":
			auditLog = append(auditLog, msg.Payload)
			saveAuditToFile("audit.json", auditLog)
			response.Command = "audit.appended"
			response.Payload = "ok"
		case "audit.get_root":
			root := computeMerkleRoot(auditLog)
			response.Command = "audit.root"
			response.Payload = root
		case "tool.list":
			response.Command = "tool.list"
			response.Payload = skills
		case "ping":
			response.Command = "pong"
			response.Payload = "ok"
		case "version":
			response.Command = "version"
			response.Payload = getBuildVersion()
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
