package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyHandshakeTrimsSecretFile(t *testing.T) {
	t.Setenv("STORE_VM_SHARED_SECRET", "")

	secretPath := filepath.Join(t.TempDir(), ".shared_secret")
	if err := os.WriteFile(secretPath, []byte("test-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", secretPath, err)
	}
	t.Setenv("STORE_VM_SHARED_SECRET_FILE", secretPath)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		errCh <- verifyHandshake(serverConn)
	}()

	if err := json.NewEncoder(clientConn).Encode(map[string]string{
		"type":   "handshake",
		"secret": "test-secret",
	}); err != nil {
		t.Fatalf("encode handshake: %v", err)
	}

	var ack map[string]string
	if err := json.NewDecoder(clientConn).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack["status"] != "ok" {
		t.Fatalf("unexpected ack: %v", ack)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("verifyHandshake() error = %v", err)
	}
}
