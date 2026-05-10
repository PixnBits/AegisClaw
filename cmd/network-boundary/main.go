package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

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

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

func isDomainAllowed(url string, allowed map[string]bool) bool {
	// Extract domain from URL
	if strings.HasPrefix(url, "http://") {
		url = url[7:]
	} else if strings.HasPrefix(url, "https://") {
		url = url[8:]
	}
	domain := strings.Split(url, "/")[0]
	return allowed[domain]
}

func runNetworkBoundary(cmd *cobra.Command, args []string) {
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
		Source:      "network-boundary",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pubStr},
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
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	fmt.Println("Network Boundary registered")

	// Load allowed domains (stub: hardcode for now)
	allowedDomains := map[string]bool{
		"example.com": true,
		"api.github.com": true,
	}

	// Start HTTP proxy
	go func() {
		http.HandleFunc("/proxy", func(w http.ResponseWriter, r *http.Request) {
			url := r.URL.Query().Get("url")
			if url == "" {
				http.Error(w, "Missing url parameter", 400)
				return
			}
			// Check domain
			if !isDomainAllowed(url, allowedDomains) {
				// Log to audit
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "blocked_request", "url": url},
					Timestamp:   "2026-05-09T20:05:01Z",
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				encoder.Encode(auditMsg)
				http.Error(w, "Domain not allowed", 403)
				return
			}
			// Inject secrets if needed (stub: for github, add token)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				http.Error(w, "Invalid URL", 400)
				return
			}
			if strings.Contains(url, "api.github.com") {
				// Inject secret (stub: hardcode)
				req.Header.Set("Authorization", "Bearer dummy_token")
			}
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				http.Error(w, "Proxy error", 500)
				return
			}
			defer resp.Body.Close()
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			// Log to audit
			auditMsg := Message{
				Source:      "network-boundary",
				Destination: "store",
				Command:     "audit.append",
				Payload:     map[string]interface{}{"action": "proxied_request", "url": url, "status": resp.StatusCode},
				Timestamp:   "2026-05-09T20:05:01Z",
				Signature:   "",
			}
			signMessage(&auditMsg, priv)
			encoder.Encode(auditMsg)
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
			Signature:   "",
		}

		switch msg.Command {
		case "network.request":
			// Handle network request (similar to proxy)
			payload := msg.Payload.(map[string]interface{})
			url := payload["url"].(string)
			method := payload["method"].(string)
			if !isDomainAllowed(url, allowedDomains) {
				response.Command = "network.response"
				response.Payload = map[string]interface{}{"error": "Domain not allowed"}
				// Audit
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "blocked_request", "url": url},
					Timestamp:   response.Timestamp,
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				encoder.Encode(auditMsg)
			} else {
				req, err := http.NewRequest(method, url, nil)
				if err != nil {
					response.Command = "network.response"
					response.Payload = map[string]interface{}{"error": "Invalid request"}
				} else {
					if strings.Contains(url, "api.github.com") {
						req.Header.Set("Authorization", "Bearer dummy_token")
					}
					client := &http.Client{}
					resp, err := client.Do(req)
					if err != nil {
						response.Command = "network.response"
						response.Payload = map[string]interface{}{"error": "Request failed"}
					} else {
						defer resp.Body.Close()
						body, _ := io.ReadAll(resp.Body)
						response.Command = "network.response"
						response.Payload = map[string]interface{}{"status": resp.StatusCode, "body": string(body)}
						// Audit
						auditMsg := Message{
							Source:      "network-boundary",
							Destination: "store",
							Command:     "audit.append",
							Payload:     map[string]interface{}{"action": "network_request", "url": url, "status": resp.StatusCode},
							Timestamp:   response.Timestamp,
							Signature:   "",
						}
						signMessage(&auditMsg, priv)
						encoder.Encode(auditMsg)
					}
				}
			}
		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}
		signMessage(&response, priv)

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
