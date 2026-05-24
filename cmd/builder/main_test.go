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
	// Test passing code
	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`
	deps := "fmt"
	if pass, _ := runSecurityGates(code, deps); !pass {
		t.Error("Should pass")
	}

	// Individual gate tests
	if pass, _ := runSAST(code); !pass {
		t.Error("Good code should pass SAST")
	}
	if pass, _ := runSecretsScan(code); !pass {
		t.Error("Good code should pass secrets scan")
	}
	if pass, _ := runPolicyCheck(code); !pass {
		t.Error("Good code should pass policy")
	}

	// Test failing SAST
	badCode := `package main

import "os/exec"

func main() {
	exec.Command("ls")
}`
	if pass, _ := runSecurityGates(badCode, deps); pass {
		t.Error("Should fail SAST")
	}

	// Test failing secrets
	secretCode := `password := "secret123"`
	if pass, msg := runSecurityGates(secretCode, deps); pass || msg == "" {
		t.Error("Should fail secrets scan")
	}

	// Test high-entropy secret (new heuristic)
	entropySecret := `key := "AKIAIOSFODNN7EXAMPLEwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`
	if pass, _ := runSecurityGates(entropySecret, deps); pass {
		t.Error("Should fail high-entropy secrets scan")
	}

	// Test that vague message is used (no details leaked)
	if pass, msg := runSecurityGates(`token := "ghp_1234567890abcdef"`, deps); !pass {
		if strings.Contains(msg, "ghp_") || strings.Contains(msg, "123456") {
			t.Error("Secrets error must be deliberately vague per builder-security-gates.md")
		}
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
