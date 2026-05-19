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
// In the current implementation this is a lightweight stub that logs the
// intent and returns a placeholder response. Real vsock forwarding will be
// added in a later phase without changing the method signature.
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