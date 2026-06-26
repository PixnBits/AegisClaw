package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"AegisClaw/internal/dashboard"
)

func TestDecodeWithContextRespectsDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	dec := json.NewDecoder(client)
	go func() {
		<-time.After(2 * time.Second)
		_, _ = server.Write([]byte(`{"command":"worker.list"}` + "\n"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var msg Message
	err := decodeWithContext(ctx, dec, &msg)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestBridgeSessionCallHonorsContext(t *testing.T) {
	server, client := net.Pipe()
	priv := ed25519.NewKeyFromSeed(make([]byte, 32))
	for i := range priv {
		priv[i] = byte(i)
	}

	sess := &bridgeSession{
		conn:    client,
		encoder: json.NewEncoder(client),
		decoder: json.NewDecoder(client),
		priv:    priv,
		viaHost: true,
	}

	go func() {
		<-time.After(2 * time.Second)
		_ = server.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := sess.call(ctx, "worker.list", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if err != context.DeadlineExceeded && !bridgeIOError(err) {
		t.Fatalf("expected bridge IO/timeout error, got %v", err)
	}
}

func TestResilientBridgeClientUsesNoopWhenDisconnected(t *testing.T) {
	r := &resilientBridgeClient{noop: &noopAPIClient{}}
	resp, err := r.Call(context.Background(), "worker.list", nil)
	if err != nil {
		t.Fatalf("noop bridge: %v", err)
	}
	if resp.Success {
		t.Fatal("expected noop failure when disconnected")
	}
}

func TestBridgeSessionSkipsHubAck(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	priv := ed25519.NewKeyFromSeed(make([]byte, 32))
	for i := range priv {
		priv[i] = byte(i + 1)
	}
	sess := &bridgeSession{
		conn:    client,
		encoder: json.NewEncoder(client),
		decoder: json.NewDecoder(client),
		priv:    priv,
		viaHost: false,
	}

	go func() {
		dec := json.NewDecoder(server)
		_ = dec.Decode(&Message{})
		enc := json.NewEncoder(server)
		_ = enc.Encode(Message{Command: "ack"})
		_ = enc.Encode(Message{Command: "worker.list", Payload: []interface{}{}})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := sess.call(ctx, "worker.list", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error %q", resp.Error)
	}
}

func TestBridgeIOError(t *testing.T) {
	if !bridgeIOError(context.DeadlineExceeded) {
		t.Fatal("deadline should count as bridge IO error")
	}
	if bridgeIOError(io.EOF) {
		t.Fatal("EOF alone should not invalidate session")
	}
}

func TestBridgeResponseErrorParsesHubACLEnvelope(t *testing.T) {
	raw := json.RawMessage(`{"error":"ERR_ACL_VIOLATION"}`)
	got := bridgeResponseError(raw, &Message{})
	if got != "ERR_ACL_VIOLATION" {
		t.Fatalf("got %q", got)
	}
}

func TestBridgeSessionSurfacesACLError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	priv := ed25519.NewKeyFromSeed(make([]byte, 32))
	sess := &bridgeSession{
		conn:    client,
		encoder: json.NewEncoder(client),
		decoder: json.NewDecoder(client),
		priv:    priv,
		viaHost: false,
	}

	go func() {
		dec := json.NewDecoder(server)
		_ = dec.Decode(&Message{})
		_ = json.NewEncoder(server).Encode(map[string]string{"error": "ERR_ACL_VIOLATION"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := sess.call(ctx, "permission.panel", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure")
	}
	if resp.Error != "ERR_ACL_VIOLATION" {
		t.Fatalf("got error %q", resp.Error)
	}
	_ = dashboard.APIResponse{}
}