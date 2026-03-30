package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ToolProxyVsockPort is the well-known vsock port the guest-agent connects to
// on the host (CID 2) for tool execution.  When a guest running inside a
// Firecracker VM connects to VMADDR_CID_HOST:ToolProxyVsockPort, Firecracker
// routes the connection to <vsock_device_path>_1026 on the host.
const ToolProxyVsockPort = 1026

// MaxToolProxyPayloadBytes is the maximum request payload the tool proxy will
// accept before rejecting the connection.
const MaxToolProxyPayloadBytes = 1024 * 1024 // 1 MB

// ToolProxyRequest is the vsock request from a guest agent to the host tool proxy.
type ToolProxyRequest struct {
	Tool string `json:"tool"`
	Args string `json:"args"`
}

// ToolProxyResponse is the host's reply to a guest ToolProxyRequest.
type ToolProxyResponse struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ToolProxy listens on per-VM vsock callback sockets and executes tool calls
// on behalf of guest agents.  This allows the ReAct loop to run entirely inside
// the agent VM: the guest calls Ollama, parses tool-call blocks, and sends tool
// execution requests back to the host via this proxy.
//
// Security properties:
//   - Listens only on per-VM <vsock_path>_1026 sockets; inaccessible outside the VM channel.
//   - Tool execution runs in the daemon process (trusted host) via ToolRegistry.
//   - Enforces a hard per-request payload cap (MaxToolProxyPayloadBytes).
//   - Each connection handles exactly one request-response pair.
type ToolProxy struct {
	registry *ToolRegistry
	logger   *zap.Logger

	mu        sync.Mutex
	listeners map[string]net.Listener // vmID -> UDS listener
}

// NewToolProxy creates a tool proxy backed by the given registry.
func NewToolProxy(registry *ToolRegistry, logger *zap.Logger) *ToolProxy {
	return &ToolProxy{
		registry:  registry,
		logger:    logger,
		listeners: make(map[string]net.Listener),
	}
}

// StartForVM starts the tool proxy for a specific VM.  vsockPath is the
// Firecracker vsock device socket (e.g. /run/aegisclaw/.../vsock.sock).
// The proxy binds to vsockPath + "_1026", which is where Firecracker delivers
// guest-initiated connections to host CID 2 port 1026.
func (p *ToolProxy) StartForVM(vmID, vsockPath string) error {
	listenPath := fmt.Sprintf("%s_%d", vsockPath, ToolProxyVsockPort)

	// Remove any stale socket left by a prior crash.
	_ = os.Remove(listenPath)

	l, err := net.Listen("unix", listenPath)
	if err != nil {
		return fmt.Errorf("tool proxy: listen for vm %s at %s: %w", vmID, listenPath, err)
	}
	// The jailed Firecracker process needs write permission to connect.
	_ = os.Chmod(listenPath, 0666)

	p.mu.Lock()
	p.listeners[vmID] = l
	p.mu.Unlock()

	go p.serveVM(vmID, l)

	p.logger.Info("tool proxy started for vm",
		zap.String("vm_id", vmID),
		zap.String("socket", listenPath),
	)
	return nil
}

// StopForVM closes the tool proxy listener for the specified VM.
func (p *ToolProxy) StopForVM(vmID string) {
	p.mu.Lock()
	l, ok := p.listeners[vmID]
	if ok {
		delete(p.listeners, vmID)
	}
	p.mu.Unlock()

	if ok {
		l.Close()
		p.logger.Info("tool proxy stopped for vm", zap.String("vm_id", vmID))
	}
}

// Stop closes every active tool proxy listener.
func (p *ToolProxy) Stop() {
	p.mu.Lock()
	ls := make([]net.Listener, 0, len(p.listeners))
	for _, l := range p.listeners {
		ls = append(ls, l)
	}
	p.listeners = make(map[string]net.Listener)
	p.mu.Unlock()

	for _, l := range ls {
		l.Close()
	}
}

func (p *ToolProxy) serveVM(vmID string, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return // listener closed
		}
		go p.handleConn(vmID, conn)
	}
}

func (p *ToolProxy) handleConn(vmID string, conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(600 * time.Second)) // 10 min for long-running tools

	limited := io.LimitReader(conn, MaxToolProxyPayloadBytes+1)
	var req ToolProxyRequest
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(ToolProxyResponse{Error: "decode: " + err.Error()})
		return
	}

	if req.Tool == "" {
		_ = json.NewEncoder(conn).Encode(ToolProxyResponse{Error: "tool name is required"})
		return
	}

	p.logger.Info("tool proxy: executing tool",
		zap.String("vm_id", vmID),
		zap.String("tool", req.Tool),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := p.registry.Execute(ctx, req.Tool, req.Args)

	resp := ToolProxyResponse{}
	if err != nil {
		resp.Error = err.Error()
		p.logger.Warn("tool proxy: tool execution failed",
			zap.String("vm_id", vmID),
			zap.String("tool", req.Tool),
			zap.Error(err),
		)
	} else {
		resp.Result = result
		p.logger.Info("tool proxy: tool executed successfully",
			zap.String("vm_id", vmID),
			zap.String("tool", req.Tool),
		)
	}

	_ = json.NewEncoder(conn).Encode(resp)
}
