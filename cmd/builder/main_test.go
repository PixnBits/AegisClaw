package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunSecurityGates(t *testing.T) {
	goodCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`
	deps := "fmt"

	if pass, _ := runSecurityGates(goodCode, deps); !pass {
		t.Error("Good code should pass all gates")
	}

	// Test failing SAST
	badSAST := `package main

import "os/exec"

func main() {
	exec.Command("ls")
}`
	if pass, _ := runSecurityGates(badSAST, deps); pass {
		t.Error("Should fail SAST")
	}

	// Test failing secrets (pattern)
	secretCode := `password := "secret123"`
	if pass, msg := runSecurityGates(secretCode, deps); pass || msg == "" {
		t.Error("Should fail secrets scan")
	}

	// Test high-entropy secret (heuristic)
	entropySecret := `key := "AKIAIOSFODNN7EXAMPLEwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`
	if pass, _ := runSecurityGates(entropySecret, deps); pass {
		t.Error("Should fail high-entropy secrets scan")
	}

	// Critical: vague message must be used, no details leaked (builder-security-gates.md)
	// Use full snippets with main() so Composition gate doesn't short-circuit before Secrets
	vagueTestCases := []string{
		`package main; func main() { token := "ghp_1234567890abcdef" }`,
		`package main; func main() { apiKey := "sk-1234567890abcdef" }`,
		`package main; func main() { password := "supersecretvalue123" }`,
	}
	for _, tc := range vagueTestCases {
		if pass, msg := runSecurityGates(tc, deps); !pass {
			if strings.Contains(msg, "ghp_") || strings.Contains(msg, "sk-") || strings.Contains(msg, "supersecret") {
				t.Errorf("Secrets error leaked details: %s", msg)
			}
			if !strings.Contains(msg, "Potential sensitive value detected – commit blocked for security reasons") {
				t.Errorf("Secrets error must use the exact vague message. Got: %s", msg)
			}
		}
	}

	// Test mixed failures produce report (SAST + Secrets)
	mixed := `package main
import "os/exec"
func main() { exec.Command("ls"); password := "verylongsecretvalue1234567890" }`
	if pass, report := runSecurityGates(mixed, deps); pass {
		t.Error("Mixed bad code should fail")
	} else if !strings.Contains(report, "SAST") || !strings.Contains(report, "sensitive value") {
		t.Errorf("Report should contain failures from multiple gates. Got: %s", report)
	}
}

func TestIndividualGates(t *testing.T) {
	good := `package main; func main() {}`

	// SAST
	if pass, _ := runSAST(good); !pass {
		t.Error("Good code should pass SAST")
	}
	if pass, _ := runSAST(`exec.Command("ls")`); pass {
		t.Error("SAST should catch exec")
	}

	// SCA
	if pass, _ := runSCA("fmt"); !pass {
		t.Error("Clean deps should pass SCA")
	}
	if pass, _ := runSCA("old-lib"); pass {
		t.Error("SCA should catch known bad dep")
	}
	if pass, _ := runSCA("module example\ngo 1.21\nrequire example.com/vulnerable-dep v1.0.0"); pass {
		t.Error("SCA should catch vulnerable-dep pattern")
	}
	if pass, _ := runSCA("module example\nrequire bad-license v1.0 // GPL-3"); pass {
		t.Error("SCA should catch license policy violation")
	}

	// Secrets
	if pass, _ := runSecretsScan(good); !pass {
		t.Error("Good code should pass secrets")
	}
	if pass, _ := runSecretsScan(`password := "foo1234567890123"`); pass {
		t.Error("Secrets should catch password")
	}

	// Policy
	if pass, _ := runPolicyCheck(good); !pass {
		t.Error("Good code should pass policy")
	}
	if pass, _ := runPolicyCheck(`net.Dial("example.com")`); pass {
		t.Error("Policy should block direct net.Dial")
	}

	// Composition
	if pass, _ := runCompositionCheck(good); !pass {
		t.Error("Code with main should pass composition")
	}
	if pass, _ := runCompositionCheck(`package main`); pass {
		t.Error("Composition should require main function")
	}
}

func TestSignMessage(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	msg := &Message{
		Source:    "test",
		Command:   "test",
		Payload:   "data",
		Timestamp: "2026-05-10T00:00:00Z",
	}
	signMessage(msg, priv)
	if msg.Signature == "" {
		t.Error("Signature not set")
	}

	data, _ := json.Marshal(Message{Source: "test", Command: "test", Payload: "data", Timestamp: "2026-05-10T00:00:00Z"})
	sigBytes, _ := base64.StdEncoding.DecodeString(msg.Signature)
	if !ed25519.Verify(pub, data, sigBytes) {
		t.Error("Signature verification failed")
	}
}
