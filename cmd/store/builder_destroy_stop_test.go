package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"
)

func TestRequestOrchestratorStopVM(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requestOrchestratorStopVM(enc, priv, time.Now().UTC().Format(time.RFC3339), map[string]interface{}{
		"id":         "builder-1",
		"builder_id": "builder-1",
		"tenant":     "tenant-a",
	})
	var msg Message
	if err := json.NewDecoder(&buf).Decode(&msg); err != nil {
		t.Fatal(err)
	}
	if msg.Source != "store" || msg.Destination != "daemon-orchestrator" {
		t.Fatalf("relay src/dst = %s -> %s", msg.Source, msg.Destination)
	}
	if msg.Command != "orchestrator.stop_vm" {
		t.Fatalf("command = %q, want orchestrator.stop_vm", msg.Command)
	}
	p := payloadMap(msg.Payload)
	if payloadString(p, "id") != "builder-1" {
		t.Fatalf("payload id = %v", msg.Payload)
	}
}

func TestBuilderStopID(t *testing.T) {
	if got := builderStopID(map[string]interface{}{"builder_id": "builder-sit-1"}); got != "builder-sit-1" {
		t.Fatalf("builder_id: got %q", got)
	}
	if got := builderStopID(nil); got != "builder" {
		t.Fatalf("empty: got %q", got)
	}
}
