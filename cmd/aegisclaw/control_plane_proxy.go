package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"go.uber.org/zap"
)

// ControlPlaneProxy is the daemon's thin abstraction layer for
// forwarding CLI-initiated operations to AegisHub.
// It translates high-level requests into mediated messages that
// AegisHub can route and ACL-enforce.
//
// Request Flow (Phase 7):
//   CLI / TUI client
//        │
//        │ 1. Sends JSON-RPC style request over Unix socket
//        ▼
//   api.Handler (daemon_handlers_extended.go, etc.)
//        │
//        │ 2. Constructs ControlPlaneRequest and calls Forward
//        ▼
//   ControlPlaneProxy.Forward(ctx, req)
//        │
//        │ 3. Forwards (currently stubbed) to AegisHub via vsock
//        ▼
//   AegisHub (MessageHub) ──► ACL check + route to target VM
//        │
//        │ 4. Target (Store VM, Agent VM, Web Portal VM, …) handles
//        ▼
//   Response flows back through the same path.
//
// This component enables the Host Daemon to act as a lightweight
// proxy rather than holding persistent state directly (post-Phase 5).
// All data access and skill/tool invocations should eventually flow
// through this path to AegisHub for mediation.
//
// Long-term: The daemon remains responsible only for VM lifecycle
// and temporary Composition Manifest publishing. Everything else
// is mediated via AegisHub.
type ControlPlaneProxy struct {
	hub    *ipc.MessageHub
	logger *zap.Logger
}

// NewControlPlaneProxy creates a new ControlPlaneProxy.
func NewControlPlaneProxy(hub *ipc.MessageHub, logger *zap.Logger) *ControlPlaneProxy {
	return &ControlPlaneProxy{
		hub:    hub,
		logger: logger,
	}
}

// ControlPlaneRequest represents a mediated request from CLI to AegisHub.
// The style is intentionally similar to skill/tool invocations rather than
// generic command objects.
//
// Example actions (resembling skill/tool names):
//   - "worker.list"
//   - "worker.status"
//   - "skill.list"
//   - "skill.status"
//   - "chat.message"
type ControlPlaneRequest struct {
	Action string          `json:"action"` // e.g. "worker.list", "skill.status"
	Data   json.RawMessage `json:"data,omitempty"`
}

// ControlPlaneResponse is the response returned after AegisHub mediation.
type ControlPlaneResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Forward sends a ControlPlaneRequest through the AegisHub mediation layer.
//
// Phase 8 implementation:
//   1. Wrap the request in an ipc.Message (Type=controlplane.request).
//   2. Call hub.RouteMessage (which performs ACL check via IdentityRegistry
//      and delivers to the hub's own handler).
//   3. Map DeliveryResult back to ControlPlaneResponse.
//
// The caller (api.Handler) is responsible for converting the
// ControlPlaneResponse back into an api.Response for the CLI client.
func (p *ControlPlaneProxy) Forward(ctx context.Context, req ControlPlaneRequest) (*ControlPlaneResponse, error) {
	if p.logger != nil {
		p.logger.Debug("ControlPlaneProxy.Forward",
			zap.String("action", req.Action))
	}

	if p.hub == nil {
		// Allow tests and offline usage to proceed with a minimal success response.
		return &ControlPlaneResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}

	// Serialize the request into the message payload.
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal ControlPlaneRequest: %w", err)
	}

	msg := &ipc.Message{
		ID:        "cp-" + req.Action,
		From:      "daemon",
		To:        ipc.MessageHubID,
		Type:      "controlplane.request",
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}

	// Respect context cancellation / deadline for robustness.
	select {
	case <-ctx.Done():
		return &ControlPlaneResponse{Success: false, Error: ctx.Err().Error()}, nil
	default:
	}

	result, err := p.hub.RouteMessage("daemon", msg)
	if err != nil {
		return &ControlPlaneResponse{Success: false, Error: err.Error()}, nil
	}
	if result == nil {
		return &ControlPlaneResponse{Success: false, Error: "empty response from hub"}, nil
	}

	// Surface both transport errors and logical failures from the backend.
	if !result.Success && result.Error != "" {
		return &ControlPlaneResponse{Success: false, Error: result.Error}, nil
	}

	return &ControlPlaneResponse{
		Success: result.Success,
		Error:   result.Error,
		Data:    result.Response,
	}, nil
}

// Close releases any resources held by the proxy.
func (p *ControlPlaneProxy) Close() error {
	return nil
}