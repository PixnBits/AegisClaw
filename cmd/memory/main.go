package main

import (
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"runtime/debug"
	"sort"
	"strings"
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

func countTokens(messages []string) int {
	count := 0
	for _, msg := range messages {
		count += len(msg) // simple char count
	}
	return count
}

func embedText(text string) []float64 {
	words := strings.Fields(strings.ToLower(text))
	vector := make([]float64, 128)
	for _, word := range words {
		hash := md5.Sum([]byte(word))
		for i := 0; i < 16 && i < len(vector); i++ {
			vector[i] += float64(hash[i])
		}
	}
	// Normalize
	mag := 0.0
	for _, v := range vector {
		mag += v * v
	}
	if mag > 0 {
		mag = math.Sqrt(mag)
		for i := range vector {
			vector[i] /= mag
		}
	}
	return vector
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	dot := 0.0
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

func runMemory(cmd *cobra.Command, args []string) {
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
		Source:      "memory",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: "2026-05-09T19:35:00Z",
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
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	fmt.Println("Memory VM registered")

	// In-memory store
	shortTerm := []string{}                   // conversation history
	longTerm := loadFromFile("longterm.json") // key: content, value: metadata
	tokenCount := 0

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
			Signature:   "",
		}

		mu.Lock()
		switch msg.Command {
		case "memory.get_context":
			// Auto summarize if over limit
			if tokenCount > 32000 {
				if len(shortTerm) > 5 {
					shortTerm = shortTerm[len(shortTerm)-5:]
					tokenCount = countTokens(shortTerm)
				}
			}
			short := shortTerm
			if len(shortTerm) > 10 {
				short = shortTerm[len(shortTerm)-10:]
			}
			long := []interface{}{}
			for _, v := range longTerm {
				long = append(long, v)
			}
			response.Command = "memory.context"
			response.Payload = map[string]interface{}{
				"short_term": short,
				"long_term":  long,
			}
		case "memory.store":
			payload := msg.Payload.(map[string]interface{})
			content := payload["content"].(string)
			longTerm[content] = payload
			saveToFile("longterm.json", longTerm)
			// Persist to Store VM
			storeMsg := Message{
				Source:      "memory",
				Destination: "store",
				Command:     "memory.store",
				Payload:     payload,
				Timestamp:   response.Timestamp,
				Signature:   "",
			}
			signMessage(&storeMsg, priv)
			encoder.Encode(storeMsg)
			response.Command = "memory.stored"
			response.Payload = "ok"
		case "memory.search":
			payload := msg.Payload.(map[string]interface{})
			query := payload["query"].(string)
			queryVector := embedText(query)
			type result struct {
				item interface{}
				sim  float64
			}
			var resList []result
			for _, v := range longTerm {
				if vec, ok := v.(map[string]interface{})["vector"].([]float64); ok {
					sim := cosine(queryVector, vec)
					if sim > 0.1 { // threshold
						resList = append(resList, result{v, sim})
					}
				}
			}
			sort.Slice(resList, func(i, j int) bool {
				return resList[i].sim > resList[j].sim
			})
			results := []interface{}{}
			limit := 5
			for i, r := range resList {
				if i >= limit {
					break
				}
				results = append(results, r.item)
			}
			response.Command = "memory.results"
			response.Payload = results
		case "memory.summarize":
			if len(shortTerm) > 5 {
				shortTerm = shortTerm[len(shortTerm)-5:]
			}
			response.Command = "memory.summarized"
			response.Payload = "ok"
		case "ping":
			response.Command = "pong"
			response.Payload = "ok"
		case "version", "get-version":
			if msg.Command == "get-version" {
				// For get-version from hub, send a proper Message response back
				response.Command = "version"
				response.Source = "memory"
				response.Destination = msg.Source  // Send back to the requester
				response.Payload = map[string]string{"version": getBuildVersion()}
				// Don't unlock here - let the normal flow sign and send
			} else {
				response.Command = "version"
				response.Payload = map[string]string{"version": getBuildVersion()}
			}
		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}
		signMessage(&response, priv)
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
