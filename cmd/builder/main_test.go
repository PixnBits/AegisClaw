package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
