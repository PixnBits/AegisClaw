package main

import (
	"context"
	"encoding/json"

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
// Flow inside Forward (current Phase 7 stub):
//   1. Log the requested action for observability.
//   2. Return a placeholder success response (real implementation will
//      serialize the request, send it over vsock to AegisHub, wait for
//      the response, and return it here).
//
// In the current implementation this is a lightweight stub that logs the
// intent and returns a placeholder response. Real vsock forwarding will be
// added in a later phase (Phase 8) without changing the method signature.
//
// The caller (api.Handler) is responsible for converting the
// ControlPlaneResponse back into an api.Response for the CLI client.
func (p *ControlPlaneProxy) Forward(ctx context.Context, req ControlPlaneRequest) (*ControlPlaneResponse, error) {
	if p.logger != nil {
		p.logger.Debug("ControlPlaneProxy.Forward (stub)",
			zap.String("action", req.Action))
	}

	// TODO(Phase 6+): Serialize req and send via vsock to AegisHub.
	// AegisHub will ACL-check and route to the appropriate microVM
	// (Store VM for worker/proposal data, Agent VMs for chat, etc.).

	// Placeholder response for now.
	return &ControlPlaneResponse{
		Success: true,
		Data:    json.RawMessage(`{}`),
	}, nil
}

// Close releases any resources held by the proxy.
func (p *ControlPlaneProxy) Close() error {
	return nil
}