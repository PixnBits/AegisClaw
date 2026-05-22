package main

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"go.uber.org/zap"
)

func TestAuthenticateConnRegistersVM(t *testing.T) {
	t.Setenv("AEGISHUB_SHARED_SECRET", "test-secret")

	srv := &server{
		logger: zap.NewNop(),
		hub:    ipc.NewMessageHubNoKernel(zap.NewNop()),
	}
	if err := srv.hub.Start(); err != nil {
		t.Fatalf("hub.Start() error = %v", err)
	}
	defer srv.hub.Stop()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		_, err := srv.authenticateConn(serverConn)
		errCh <- err
	}()

	if err := json.NewEncoder(clientConn).Encode(handshakeRequest{
		Type:   "handshake",
		Secret: "test-secret",
		VMID:   "skill-1",
		Role:   string(ipc.RoleSkill),
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
		t.Fatalf("authenticateConn() error = %v", err)
	}
}

func TestDispatchRoutesWithAuthenticatedSender(t *testing.T) {
	srv := &server{
		logger: zap.NewNop(),
		hub:    ipc.NewMessageHubNoKernel(zap.NewNop()),
	}
	if err := srv.hub.Start(); err != nil {
		t.Fatalf("hub.Start() error = %v", err)
	}
	defer srv.hub.Stop()

	if err := srv.hub.RegisterVM("skill-1", ipc.RoleSkill); err != nil {
		t.Fatalf("RegisterVM(skill-1) error = %v", err)
	}

	var received *ipc.Message
	if err := srv.hub.RegisterSkill("daemon-1", func(msg *ipc.Message) (*ipc.DeliveryResult, error) {
		received = msg
		return &ipc.DeliveryResult{MessageID: msg.ID, Success: true}, nil
	}); err != nil {
		t.Fatalf("RegisterSkill(daemon-1) error = %v", err)
	}

	payload, err := json.Marshal(ipc.Message{
		ID:   "msg-1",
		To:   "daemon-1",
		Type: "tool.result",
	})
	if err != nil {
		t.Fatalf("marshal message payload: %v", err)
	}

	resp := srv.dispatch(authenticatedVM{ID: "skill-1", Role: ipc.RoleSkill}, HubRequest{
		ID:      "req-1",
		Type:    "route",
		Payload: payload,
	})

	if !resp.Success {
		t.Fatalf("dispatch() failed: %s", resp.Error)
	}
	if received == nil {
		t.Fatal("expected registered handler to receive routed message")
	}
	if received.From != "skill-1" {
		t.Fatalf("received.From = %q, want skill-1", received.From)
	}
}
