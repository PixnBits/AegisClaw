package ipc

import (
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// Bridge connects the kernel's ControlPlane to the MessageHub, translating
// vsock control messages into IPC messages and routing them through the hub.
type Bridge struct {
	hub    *MessageHub
	kern   *kernel.Kernel
	logger *zap.Logger
}

// NewBridge creates a bridge between the control plane and message hub.
func NewBridge(hub *MessageHub, kern *kernel.Kernel, logger *zap.Logger) *Bridge {
	return &Bridge{
		hub:    hub,
		kern:   kern,
		logger: logger,
	}
}

// RegisterControlPlaneHandlers installs message routing handlers on the kernel's
// control plane so that VMs can send IPC messages via their vsock connection.
func (b *Bridge) RegisterControlPlaneHandlers() error {
	// Handle "ipc.send" messages from VMs
	if err := b.kern.ControlPlane().RegisterHandler("ipc.send", b.handleIPCSend); err != nil {
		return fmt.Errorf("failed to register ipc.send handler: %w", err)
	}

	// Handle "ipc.routes" queries from VMs
	if err := b.kern.ControlPlane().RegisterHandler("ipc.routes", b.handleIPCRoutes); err != nil {
		return fmt.Errorf("failed to register ipc.routes handler: %w", err)
	}

	b.logger.Info("IPC bridge control plane handlers registered")
	return nil
}

func (b *Bridge) handleIPCSend(vmID string, ctlMsg kernel.ControlMessage) (*kernel.ControlResponse, error) {
	var msg Message
	if err := json.Unmarshal(ctlMsg.Payload, &msg); err != nil {
		return &kernel.ControlResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid IPC message: %v", err),
		}, nil
	}

	result, err := b.hub.RouteMessage(vmID, &msg)
	if err != nil {
		return &kernel.ControlResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	data, _ := json.Marshal(result)
	return &kernel.ControlResponse{
		Success: result.Success,
		Error:   result.Error,
		Data:    data,
	}, nil
}

func (b *Bridge) handleIPCRoutes(vmID string, ctlMsg kernel.ControlMessage) (*kernel.ControlResponse, error) {
	routes := b.hub.Router().RegisteredRoutes()
	data, _ := json.Marshal(routes)
	return &kernel.ControlResponse{
		Success: true,
		Data:    data,
	}, nil
}
