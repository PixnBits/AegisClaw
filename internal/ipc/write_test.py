#!/usr/bin/env python3
"""Writes IPC package tests."""

code = r'''package ipc

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantErr bool
	}{
		{
			name: "valid message",
			msg: Message{
				ID:        "msg-1",
				From:      "skill-a",
				To:        "skill-b",
				Type:      "request",
				Timestamp: time.Now(),
			},
			wantErr: false,
		},
		{name: "missing ID", msg: Message{From: "a", To: "b", Type: "t"}, wantErr: true},
		{name: "missing From", msg: Message{ID: "1", To: "b", Type: "t"}, wantErr: true},
		{name: "missing To", msg: Message{ID: "1", From: "a", Type: "t"}, wantErr: true},
		{name: "missing Type", msg: Message{ID: "1", From: "a", To: "b"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRouter_RegisterAndRoute(t *testing.T) {
	r := NewRouter()

	received := make(chan *Message, 1)
	handler := func(msg *Message) (*DeliveryResult, error) {
		received <- msg
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   true,
			Response:  json.RawMessage(`{"ok":true}`),
		}, nil
	}

	if err := r.Register("skill-b", handler); err != nil {
		t.Fatalf("Register: %v", err)
	}

	msg := &Message{
		ID:        "msg-1",
		From:      "skill-a",
		To:        "skill-b",
		Type:      "request",
		Timestamp: time.Now(),
	}

	result, err := r.Route("skill-a", msg)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	select {
	case got := <-received:
		if got.ID != "msg-1" {
			t.Fatalf("expected msg-1, got %s", got.ID)
		}
	default:
		t.Fatal("handler was not called")
	}
}

func TestRouter_SenderValidation(t *testing.T) {
	r := NewRouter()
	r.Register("skill-b", func(msg *Message) (*DeliveryResult, error) {
		return &DeliveryResult{MessageID: msg.ID, Success: true}, nil
	})

	msg := &Message{
		ID:        "msg-1",
		From:      "skill-a",
		To:        "skill-b",
		Type:      "request",
		Timestamp: time.Now(),
	}

	// Route with wrong sender identity — should be rejected
	_, err := r.Route("skill-IMPOSTER", msg)
	if err == nil {
		t.Fatal("expected error for sender mismatch")
	}
}

func TestRouter_SelfRouting(t *testing.T) {
	r := NewRouter()
	r.Register("skill-a", func(msg *Message) (*DeliveryResult, error) {
		return &DeliveryResult{MessageID: msg.ID, Success: true}, nil
	})

	msg := &Message{
		ID:        "msg-1",
		From:      "skill-a",
		To:        "skill-a",
		Type:      "echo",
		Timestamp: time.Now(),
	}

	_, err := r.Route("skill-a", msg)
	if err == nil {
		t.Fatal("expected error for self-routing")
	}
}

func TestRouter_NoRoute(t *testing.T) {
	r := NewRouter()

	msg := &Message{
		ID:        "msg-1",
		From:      "skill-a",
		To:        "nonexistent",
		Type:      "request",
		Timestamp: time.Now(),
	}

	result, err := r.Route("skill-a", msg)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for missing route")
	}
}

func TestRouter_DuplicateRegister(t *testing.T) {
	r := NewRouter()
	handler := func(msg *Message) (*DeliveryResult, error) {
		return &DeliveryResult{MessageID: msg.ID, Success: true}, nil
	}

	if err := r.Register("skill-a", handler); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := r.Register("skill-a", handler)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRouter_Unregister(t *testing.T) {
	r := NewRouter()
	r.Register("skill-a", func(msg *Message) (*DeliveryResult, error) {
		return &DeliveryResult{MessageID: msg.ID, Success: true}, nil
	})

	if !r.HasRoute("skill-a") {
		t.Fatal("expected route to exist")
	}

	r.Unregister("skill-a")

	if r.HasRoute("skill-a") {
		t.Fatal("expected route to be removed")
	}
}

func TestRouter_RegisteredRoutes(t *testing.T) {
	r := NewRouter()
	handler := func(msg *Message) (*DeliveryResult, error) {
		return &DeliveryResult{MessageID: msg.ID, Success: true}, nil
	}

	r.Register("alpha", handler)
	r.Register("beta", handler)
	r.Register("gamma", handler)

	routes := r.RegisteredRoutes()
	if len(routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(routes))
	}

	routeMap := make(map[string]bool)
	for _, id := range routes {
		routeMap[id] = true
	}
	for _, expected := range []string{"alpha", "beta", "gamma"} {
		if !routeMap[expected] {
			t.Fatalf("missing route: %s", expected)
		}
	}
}
'''

with open('router_test.go', 'w') as f:
    f.write(code)
print(f"Written {len(code)} bytes")
