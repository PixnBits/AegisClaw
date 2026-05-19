package main

import (
	"context"
	"encoding/json"
	"testing"

	"go.uber.org/zap"
)

// TestControlPlaneProxy_New verifies that NewControlPlaneProxy constructs
// a non-nil proxy with the provided dependencies.
func TestControlPlaneProxy_New(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)
	if proxy == nil {
		t.Fatal("NewControlPlaneProxy returned nil")
	}
}

// TestControlPlaneProxy_Forward_Basic exercises the happy-path stub
// and verifies that a success response with empty data is returned.
func TestControlPlaneProxy_Forward_Basic(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)

	req := ControlPlaneRequest{
		Action: "worker.list",
		Data:   json.RawMessage(`{}`),
	}

	resp, err := proxy.Forward(context.Background(), req)
	if err != nil {
		t.Fatalf("Forward returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response")
	}
	if !resp.Success {
		t.Errorf("expected Success=true, got false (error=%s)", resp.Error)
	}
}

// TestControlPlaneProxy_Forward_ErrorHandling verifies that the proxy
// currently does not return errors for unknown actions (stub behavior).
// Future real implementation may return errors for ACL failures, etc.
func TestControlPlaneProxy_Forward_ErrorHandling(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)

	// Table-driven cases for different actions.
	cases := []struct {
		action string
	}{
		{"worker.list"},
		{"skill.status"},
		{"chat.message"},
	}

	for _, c := range cases {
		req := ControlPlaneRequest{Action: c.action}
		resp, err := proxy.Forward(context.Background(), req)
		if err != nil {
			t.Errorf("Forward(%s) unexpected error: %v", c.action, err)
			continue
		}
		if resp == nil || !resp.Success {
			t.Errorf("Forward(%s) expected success response, got %+v", c.action, resp)
		}
	}
}

// TestControlPlaneProxy_HandlerUsagePattern demonstrates how a wired
// handler would use the proxy (example for Phase 7 integration tests).
func TestControlPlaneProxy_HandlerUsagePattern(t *testing.T) {
	logger := zap.NewNop()
	proxy := NewControlPlaneProxy(nil, logger)

	// Simulate what a handler does:
	data := json.RawMessage(`{"active_only": true}`)
	resp, err := proxy.Forward(context.Background(), ControlPlaneRequest{
		Action: "worker.list",
		Data:   data,
	})
	if err != nil {
		t.Fatalf("handler simulation Forward error: %v", err)
	}
	if resp == nil {
		t.Fatal("handler simulation got nil response")
	}
}