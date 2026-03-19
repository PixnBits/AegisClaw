package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"go.uber.org/zap"
)

// ControlMessage represents a JSON message received from a guest VM via vsock.
type ControlMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ControlResponse is the kernel's response sent back to a guest VM.
type ControlResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MessageHandler processes a control message from a specific VM.
// The vmID identifies which VM sent the message.
type MessageHandler func(vmID string, msg ControlMessage) (*ControlResponse, error)

// ControlPlane manages vsock-based communication between the kernel and guest VMs.
// Firecracker exposes each VM's vsock as a Unix domain socket on the host.
// The kernel listens on these sockets and routes messages through handlers,
// with every message signed and logged via the kernel's SignAndLog.
type ControlPlane struct {
	kernel    *Kernel
	handlers  map[string]MessageHandler
	listeners map[string]net.Listener
	mu        sync.RWMutex
	logger    *zap.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewControlPlane creates a ControlPlane bound to the given kernel.
func NewControlPlane(kernel *Kernel, logger *zap.Logger) *ControlPlane {
	ctx, cancel := context.WithCancel(context.Background())
	return &ControlPlane{
		kernel:    kernel,
		handlers:  make(map[string]MessageHandler),
		listeners: make(map[string]net.Listener),
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// RegisterHandler registers a handler for a given control message type.
// Handlers are invoked when a guest VM sends a message with a matching type.
func (cp *ControlPlane) RegisterHandler(msgType string, handler MessageHandler) error {
	if msgType == "" {
		return fmt.Errorf("message type must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("handler must not be nil")
	}

	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.handlers[msgType] = handler
	cp.logger.Info("control plane handler registered", zap.String("message_type", msgType))
	return nil
}

// ListenForVM starts accepting connections on the Firecracker vsock UDS for a VM.
// The socketPath is the host-side Unix domain socket that Firecracker creates
// for the VM's vsock device.
func (cp *ControlPlane) ListenForVM(vmID string, socketPath string) error {
	if vmID == "" {
		return fmt.Errorf("vm ID must not be empty")
	}
	if socketPath == "" {
		return fmt.Errorf("socket path must not be empty")
	}

	cp.mu.RLock()
	_, exists := cp.listeners[vmID]
	cp.mu.RUnlock()
	if exists {
		return fmt.Errorf("listener already exists for VM %s", vmID)
	}

	// Remove stale socket file if it exists from a previous run
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket %s: %w", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on vsock UDS for VM %s at %s: %w", vmID, socketPath, err)
	}

	cp.mu.Lock()
	cp.listeners[vmID] = listener
	cp.mu.Unlock()

	go cp.acceptLoop(vmID, listener)

	cp.logger.Info("control plane listening for VM",
		zap.String("vm_id", vmID),
		zap.String("socket", socketPath),
	)
	return nil
}

// StopListeningForVM closes the listener for the specified VM.
func (cp *ControlPlane) StopListeningForVM(vmID string) error {
	cp.mu.Lock()
	listener, exists := cp.listeners[vmID]
	if exists {
		delete(cp.listeners, vmID)
	}
	cp.mu.Unlock()

	if !exists {
		return fmt.Errorf("no listener found for VM %s", vmID)
	}

	if err := listener.Close(); err != nil {
		return fmt.Errorf("failed to close listener for VM %s: %w", vmID, err)
	}

	cp.logger.Info("stopped listening for VM", zap.String("vm_id", vmID))
	return nil
}

// ActiveListeners returns the number of currently active VM listeners.
func (cp *ControlPlane) ActiveListeners() int {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return len(cp.listeners)
}

// Send sends a control message to a VM via its vsock UDS and waits for a response.
func (cp *ControlPlane) Send(vmID string, msg ControlMessage) (*ControlResponse, error) {
	cp.mu.RLock()
	listener, exists := cp.listeners[vmID]
	cp.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no listener for VM %s", vmID)
	}

	addr := listener.Addr()
	conn, err := net.Dial(addr.Network(), addr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to VM %s: %w", vmID, err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send message to VM %s: %w", vmID, err)
	}

	var resp ControlResponse
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response from VM %s: %w", vmID, err)
	}

	return &resp, nil
}

// Shutdown closes all listeners and cancels the control plane context.
func (cp *ControlPlane) Shutdown() {
	cp.cancel()
	cp.mu.Lock()
	defer cp.mu.Unlock()
	for vmID, listener := range cp.listeners {
		if err := listener.Close(); err != nil {
			cp.logger.Warn("error closing listener during shutdown",
				zap.String("vm_id", vmID),
				zap.Error(err),
			)
		}
		delete(cp.listeners, vmID)
	}
	cp.logger.Info("control plane shut down")
}

func (cp *ControlPlane) acceptLoop(vmID string, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-cp.ctx.Done():
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				cp.logger.Error("failed to accept vsock connection",
					zap.String("vm_id", vmID),
					zap.Error(err),
				)
				return
			}
		}
		go cp.handleConnection(vmID, conn)
	}
}

func (cp *ControlPlane) handleConnection(vmID string, conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		select {
		case <-cp.ctx.Done():
			return
		default:
		}

		var msg ControlMessage
		if err := decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			cp.logger.Error("failed to decode control message",
				zap.String("vm_id", vmID),
				zap.Error(err),
			)
			errResp := &ControlResponse{
				Success: false,
				Error:   "malformed message",
			}
			encoder.Encode(errResp)
			return
		}

		if msg.Type == "" {
			resp := &ControlResponse{
				Success: false,
				Error:   "message type is required",
			}
			if encErr := encoder.Encode(resp); encErr != nil {
				cp.logger.Error("failed to send error response",
					zap.String("vm_id", vmID),
					zap.Error(encErr),
				)
				return
			}
			continue
		}

		// Sign and log every control plane message through the kernel
		payloadBytes, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			cp.logger.Error("failed to marshal control message for audit",
				zap.String("vm_id", vmID),
				zap.Error(marshalErr),
			)
			continue
		}

		action := NewAction(ActionControlPlane, vmID, payloadBytes)
		if _, signErr := cp.kernel.SignAndLog(action); signErr != nil {
			cp.logger.Error("failed to sign and log control message",
				zap.String("vm_id", vmID),
				zap.Error(signErr),
			)
		}

		// Route message to registered handler
		cp.mu.RLock()
		handler, handlerExists := cp.handlers[msg.Type]
		cp.mu.RUnlock()

		var resp *ControlResponse
		if !handlerExists {
			resp = &ControlResponse{
				Success: false,
				Error:   fmt.Sprintf("no handler registered for message type: %s", msg.Type),
			}
		} else {
			var handlerErr error
			resp, handlerErr = handler(vmID, msg)
			if handlerErr != nil {
				cp.logger.Error("handler error",
					zap.String("vm_id", vmID),
					zap.String("message_type", msg.Type),
					zap.Error(handlerErr),
				)
				resp = &ControlResponse{
					Success: false,
					Error:   handlerErr.Error(),
				}
			}
		}

		if encErr := encoder.Encode(resp); encErr != nil {
			cp.logger.Error("failed to send control response",
				zap.String("vm_id", vmID),
				zap.Error(encErr),
			)
			return
		}
	}
}
