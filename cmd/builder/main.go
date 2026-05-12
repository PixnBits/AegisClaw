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
	"regexp"
	"runtime/debug"
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

func runSAST(code string) (bool, string) {
	// Check for unsafe patterns
	patterns := []string{
		`eval\s*\(`,
		`exec\.Command`,
		`system\s*\(`,
		`os\.popen`,
		`subprocess\.call`,
	}
	for _, pat := range patterns {
		if matched, _ := regexp.MatchString(pat, code); matched {
			return false, "SAST: Unsafe code pattern detected"
		}
	}
	return true, ""
}

func runSCA(deps string) (bool, string) {
	// Stub: check for known vulnerable deps
	if strings.Contains(deps, "old-lib") {
		return false, "SCA: Vulnerable dependency detected"
	}
	return true, ""
}

func runSecretsScan(code string) (bool, string) {
	// Scan for potential secrets: high-entropy strings, patterns
	secretPatterns := []string{
		`(?i)password\s*[:=]\s*['"]\w+['"]`,
		`(?i)token\s*[:=]\s*['"]\w+['"]`,
		`(?i)secret\s*[:=]\s*['"]\w+['"]`,
	}
	for _, pat := range secretPatterns {
		if matched, _ := regexp.MatchString(pat, code); matched {
			return false, "Potential sensitive value detected – commit blocked for security reasons"
		}
	}
	return true, ""
}

func runPolicyCheck(code string) (bool, string) {
	// Check policies: no direct network, etc.
	if strings.Contains(code, "net.Dial") && !strings.Contains(code, "network-boundary") {
		return false, "Policy: Direct network access not allowed"
	}
	return true, ""
}

func runCompositionCheck(code string) (bool, string) {
	// Basic checks: has main, etc.
	if !strings.Contains(code, "func main") {
		return false, "Composition: Missing main function"
	}
	return true, ""
}

func runSecurityGates(code, deps string) (bool, string) {
	var report []string

	if pass, msg := runSAST(code); !pass {
		report = append(report, msg)
	}
	if pass, msg := runSCA(deps); !pass {
		report = append(report, msg)
	}
	if pass, msg := runSecretsScan(code); !pass {
		report = append(report, msg)
	}
	if pass, msg := runPolicyCheck(code); !pass {
		report = append(report, msg)
	}
	if pass, msg := runCompositionCheck(code); !pass {
		report = append(report, msg)
	}

	if len(report) > 0 {
		return false, strings.Join(report, "; ")
	}
	return true, ""
}

func generateSkillCode(description string) string {
	prompt := "Generate Go code for a skill based on this description: " + description + ". Include main function, error handling, and basic tests."
	return callLLM(prompt)
}

func callLLM(prompt string) string {
	// Mock LLM response
	return `package main

import "fmt"

func main() {
	fmt.Println("Skill executed")
}`
}

func runBuilder(cmd *cobra.Command, args []string) {
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
		Source:      "builder",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: "2026-05-09T20:00:00Z",
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
			Signature:   "",
		}

		switch msg.Command {
		case "store.git.clone":
			// Implement git clone
			payload := msg.Payload.(map[string]interface{})
			repo := payload["repo"].(string)
			// Simulate clone
			response.Command = "git.cloned"
			response.Payload = map[string]interface{}{"repo": repo, "status": "cloned"}
		case "store.git.push":
			// Run security gates before push
			payload := msg.Payload.(map[string]interface{})
			code := payload["code"].(string)
			deps := payload["deps"].(string)
			if pass, report := runSecurityGates(code, deps); !pass {
				response.Command = "git.push_failed"
				response.Payload = report
				// Audit failure
				auditMsg := Message{
					Source:      "builder",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "push_blocked", "reason": report},
					Timestamp:   response.Timestamp,
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				encoder.Encode(auditMsg)
			} else {
				response.Command = "git.pushed"
				response.Payload = "ok"
				// Audit success
				auditMsg := Message{
					Source:      "builder",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "push_allowed"},
					Timestamp:   response.Timestamp,
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				encoder.Encode(auditMsg)
			}
		case "store.pr.create":
			response.Command = "pr.created"
			response.Payload = "ok"
		case "builder.implement_skill":
			payload := msg.Payload.(map[string]interface{})
			description := payload["description"].(string)
			code := generateSkillCode(description)
			// Run security gates
			if pass, report := runSecurityGates(code, ""); !pass {
				response.Command = "implementation.failed"
				response.Payload = report
			} else {
				response.Command = "implementation.success"
				response.Payload = map[string]interface{}{
					"code":  code,
					"tests": "basic tests", // stub
				}
			}
		case "version", "get-version":
			if msg.Command == "get-version" {
				// For get-version from hub, send proper Message response back
				response.Command = "version"
				response.Source = "builder"
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
		signMessage(&response, priv)

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
