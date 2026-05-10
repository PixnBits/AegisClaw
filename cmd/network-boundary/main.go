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
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var allowedForwardHeaders = map[string]bool{
	"Accept":       true,
	"Content-Type": true,
}

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

func isDomainAllowed(rawURL string, allowed map[string]bool) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	return allowed[parsed.Host]
}

func parseAllowedURL(rawURL string, allowed map[string]bool) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme")
	}
	if !allowed[parsed.Host] {
		return nil, fmt.Errorf("domain not allowed")
	}
	return parsed, nil
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
		"example.com":     true,
		"api.github.com":  true,
		"localhost:11434": true,
	}
	ollamaGenerateURL := "http://localhost:11434/api/generate"

	// Start HTTP proxy
	go func() {
		http.HandleFunc("/proxy/ollama/generate", func(w http.ResponseWriter, r *http.Request) {
			targetURL := ollamaGenerateURL
			if legacyURL := r.URL.Query().Get("url"); legacyURL != "" && legacyURL != ollamaGenerateURL {
				http.Error(w, "Domain not allowed", 403)
				return
			}

			parsedURL, err := parseAllowedURL(targetURL, allowedDomains)
			if err != nil {
				// Log to audit
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "blocked_request", "url": targetURL},
					Timestamp:   "2026-05-09T20:05:01Z",
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				encoder.Encode(auditMsg)
				http.Error(w, "Domain not allowed", 403)
				return
			}
			// Inject secrets if needed (stub: for github, add token)
			req, err := http.NewRequest(r.Method, parsedURL.String(), r.Body)
			if err != nil {
				http.Error(w, "Invalid URL", 400)
				return
			}
			for header := range allowedForwardHeaders {
				if val := r.Header.Get(header); val != "" {
					req.Header.Set(header, val)
				}
			}
			if strings.Contains(parsedURL.Host, "api.github.com") {
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
				Payload:     map[string]interface{}{"action": "proxied_request", "url": targetURL, "status": resp.StatusCode},
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
			targetURL := payload["url"].(string)
			method, _ := payload["method"].(string)
			if strings.TrimSpace(method) == "" {
				method = http.MethodGet
			}
			parsedURL, err := parseAllowedURL(targetURL, allowedDomains)
			if err != nil {
				response.Command = "network.response"
				response.Payload = map[string]interface{}{"error": err.Error()}
				// Audit
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "blocked_request", "url": targetURL},
					Timestamp:   response.Timestamp,
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				encoder.Encode(auditMsg)
			} else {
				var bodyReader io.Reader
				if body, ok := payload["body"].(string); ok {
					bodyReader = strings.NewReader(body)
				}

				req, err := http.NewRequest(method, parsedURL.String(), bodyReader)
				if err != nil {
					response.Command = "network.response"
					response.Payload = map[string]interface{}{"error": "Invalid request"}
				} else {
					if headers, ok := payload["headers"].(map[string]interface{}); ok {
						for k, v := range headers {
							if allowedForwardHeaders[k] {
								req.Header.Set(k, fmt.Sprintf("%v", v))
							}
						}
					}
					if strings.Contains(parsedURL.Host, "api.github.com") {
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
								Payload:     map[string]interface{}{"action": "network_request", "url": targetURL, "status": resp.StatusCode},
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
