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
	// Per builder-security-gates.md:8-10 — detect common vuln patterns, unsafe practices.
	// Go-focused for Phase 4; designed to be extensible for language-specific rules.
	patterns := []string{
		`eval\s*\(`,
		`exec\.Command`,
		`system\s*\(`,
		`os\.popen`,
		`subprocess\.call`,
		`unsafe\.Pointer`,
		`//go:linkname`,
		`http\.ListenAndServe\s*\(\s*":\d+"`, // direct listen without config
	}
	for _, pat := range patterns {
		if matched, _ := regexp.MatchString(pat, code); matched {
			return false, "SAST: Unsafe code pattern detected"
		}
	}
	return true, ""
}

func runSCA(deps string) (bool, string) {
	// Per builder-security-gates.md:12-14 — SCA + license policy.
	// Phase 4: basic known-bad + license checks. Real scanner integration later.
	if strings.Contains(strings.ToLower(deps), "old-lib") || strings.Contains(deps, "vulnerable-dep") {
		return false, "SCA: Vulnerable dependency detected"
	}
	if strings.Contains(strings.ToLower(deps), "gpl-3") { // example license policy
		return false, "SCA: License policy violation"
	}
	return true, ""
}

func runSecretsScan(code string) (bool, string) {
	// Per builder-security-gates.md:16-20 — block ANY potential secret/high-entropy value.
	// Use multiple methods (patterns + simple entropy heuristic). Deliberately vague error only.
	secretPatterns := []string{
		`(?i)password\s*[:=]+\s*['"]?[\w.-]+['"]?`,
		`(?i)token\s*[:=]+\s*['"]?[\w.-]+['"]?`,
		`(?i)secret\s*[:=]+\s*['"]?[\w.-]+['"]?`,
		`(?i)api[_-]?key\s*[:=]+\s*['"]?[\w.-]+['"]?`,
		`(?i)bearer\s+['"]?[\w.-]{20,}`,
	}

	for _, pat := range secretPatterns {
		if matched, _ := regexp.MatchString(pat, code); matched {
			return false, "Potential sensitive value detected – commit blocked for security reasons"
		}
	}

	// Simple high-entropy heuristic (long base64-like strings)
	if matched, _ := regexp.MatchString(`[A-Za-z0-9+/=]{32,}`, code); matched {
		return false, "Potential sensitive value detected – commit blocked for security reasons"
	}

	return true, ""
}

func runPolicyCheck(code string) (bool, string) {
	// Per builder-security-gates.md:22-24 — Policy-as-Code (simple rules for now; future: Rego).
	// Examples from spec: must route outbound through Network Boundary, no direct credentials.
	if strings.Contains(code, "net.Dial") && !strings.Contains(code, "network-boundary") {
		return false, "Policy: Direct network access not allowed — must use Network Boundary"
	}
	if strings.Contains(code, "os.Getenv") && strings.Contains(code, "token") {
		return false, "Policy: Direct credential access not allowed"
	}
	return true, ""
}

func runCompositionCheck(code string) (bool, string) {
	// Per builder-security-gates.md:26-29 — artifact integrity + basic health.
	// For Phase 4: minimal structural checks. Real smoke + rollback in later wiring.
	if !strings.Contains(code, "func main") {
		return false, "Composition: Missing main function"
	}
	return true, ""
}

func runSecurityGates(code, deps string) (bool, string) {
	// Strict sequential order per builder-security-gates.md:6-30.
	// Any failure stops further gates for that build (fail-fast) but we collect for report.
	// Failure report must be detailed for Court but non-leaking for secrets.
	var report []string

	gates := []struct {
		name string
		fn   func(string, string) (bool, string)
	}{
		{"SAST", func(c, d string) (bool, string) { return runSAST(c) }},
		{"SCA", func(c, d string) (bool, string) { return runSCA(d) }},
		{"Secrets", func(c, d string) (bool, string) { return runSecretsScan(c) }},
		{"Policy", func(c, d string) (bool, string) { return runPolicyCheck(c) }},
		{"Composition", func(c, d string) (bool, string) { return runCompositionCheck(c) }},
	}

	for _, g := range gates {
		pass, msg := g.fn(code, deps)
		if !pass {
			report = append(report, msg)
			// Per spec: on any failure the build is Failed. We still run remaining for fuller report in Phase 4.
		}
	}

	if len(report) > 0 {
		return false, strings.Join(report, " | ")
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
	var connMutex sync.Mutex

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
	fmt.Println("Builder VM registered")

	// Builder loop
	for {
		var msg Message
		connMutex.Lock()
		err := decoder.Decode(&msg)
		connMutex.Unlock()
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
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()
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
		Use:   "builder",
		Short: "Builder VM",
		Run:   runBuilder,
	}

	rootCmd.Execute()
}
