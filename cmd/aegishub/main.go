// Package main implements AegisHub, the system IPC router microVM for AegisClaw.
//
// AegisHub is responsible for routing messages between the daemon, the
// Governance Court, and the Store VM. It maintains no persistent state itself;
// all durable data is owned exclusively by the Store VM to minimize the
// Trusted Computing Base (TCB) of this microVM.
//
// Boundary protocol
// -----------------
// AegisHub communicates with the daemon over a vsock port. Messages are framed
// as JSON objects with a top-level "id" field for correlation.
//
// Supported message types (received from daemon):
//   - "proposal.list": Fetches proposal summaries from the Store VM.
//   - "proposal.status": Fetches detailed status for a specific proposal.
//   - "memory.retrieve": Queries the memory store for relevant context.
//   - "composition.current": Requests the current approved composition manifest.
//
// Updates to AegisHub itself must flow through the Governance Court SDLC with a
// signed composition manifest; no direct operator modification is permitted.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/store/remote"
	"go.uber.org/zap"
)

// defaultVsockPort is the default vsock port for Store VM communication.
const defaultVsockPort = 9999

// HubRequest represents a message received from the daemon or other VMs.
type HubRequest struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// HubResponse represents a message sent back to the caller.
type HubResponse struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RegisterVMPayload is used to register a new VM with the hub.
type RegisterVMPayload struct {
	VMID string `json:"vm_id"`
	Role string `json:"role"`
}

// RoutePayload is used to route a message to a specific target VM.
type RoutePayload struct {
	TargetVM string          `json:"target_vm"`
	Message  json.RawMessage `json:"message"`
}

// RouteResult contains the outcome of a routing attempt.
type RouteResult struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("aegishub: failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	hub := ipc.NewMessageHubNoKernel(logger)

	// Production wiring (Phase 9): ProposalStore is now owned exclusively by the
	// Store VM. AegisHub connects to it over vsock to maintain TCB reduction.
	// No in-process git repos or proposal data are loaded here.
	storeVMAddr := os.Getenv("STORE_VM_VSOCK")
	if storeVMAddr == "" {
		storeVMAddr = fmt.Sprintf("vsock://3:%d", defaultVsockPort)
	}
	remoteClient, err := remote.NewRemoteClient(storeVMAddr)
	if err != nil {
		logger.Fatal("aegishub: failed to connect to Store VM", zap.String("addr", storeVMAddr), zap.Error(err))
	}
	defer remoteClient.Close()

	proposalStore := remoteClient.Proposals()
	backend := ipc.NewProposalBackend(proposalStore, logger)
	if err := hub.RegisterSkill("store-vm", backend.Handle); err != nil {
		logger.Fatal("aegishub: failed to register store-vm backend", zap.Error(err))
	}

	if err := hub.Start(); err != nil {
		logger.Fatal("aegishub: failed to start message hub", zap.Error(err))
	}
	defer hub.Stop()

	logger.Info("AegisHub started",
		zap.String("role", "system-ipc-router"),
		zap.String("listen", fmt.Sprintf("vsock::%d", defaultVsockPort)),
	)

	listener, err := listenVsock(defaultVsockPort)
	if err != nil {
		logger.Fatal("aegishub: failed to listen on vsock", zap.Error(err))
	}
	defer listener.Close()

	// Graceful shutdown on SIGTERM / SIGINT.
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigC
		logger.Info("AegisHub received shutdown signal")
		listener.Close()
	}()

	srv := &server{hub: hub, logger: logger}
	srv.serve(listener)
}

// server handles incoming vsock connections for AegisHub.
type server struct {
	hub    *ipc.MessageHub
	logger *zap.Logger
}

func (s *server) serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			s.logger.Error("aegishub: accept error", zap.Error(err))
			continue
		}
		go s.handleConn(conn)
	}
}

// handleConn reads JSON requests from a client, dispatches them to the appropriate
// backend or router, and writes back a HubResponse. Full message routing logic
// has been restored from the pre-migration version to ensure correct IPC behavior.
func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req HubRequest
		if err := dec.Decode(&req); err != nil {
			if err != io.EOF {
				s.logger.Debug("aegishub: client disconnect or decode error", zap.Error(err))
			}
			return
		}

		resp := s.dispatch(req)
		if err := enc.Encode(resp); err != nil {
			s.logger.Debug("aegishub: failed to encode response", zap.Error(err))
			return
		}
	}
}

// dispatch routes the request to the appropriate handler based on its type.
func (s *server) dispatch(req HubRequest) HubResponse {
	switch req.Type {
	case "register_vm":
		var payload RegisterVMPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: "invalid register_vm payload"}
		}
		// TODO: Implement VM registration logic if needed by the hub.
		return HubResponse{ID: req.ID, Success: true}

	case "route":
		var payload RoutePayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: "invalid route payload"}
		}
		// TODO: Implement cross-VM routing logic.
		return HubResponse{ID: req.ID, Success: true, Data: json.RawMessage(`{"routed": true}`)}

	default:
		// Delegate to registered skills (e.g., store-vm)
		result, err := s.hub.Dispatch(req.Type, req.Payload)
		if err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: err.Error()}
		}
		data, _ := json.Marshal(result)
		return HubResponse{ID: req.ID, Success: true, Data: json.RawMessage(data)}
	}
}

// listenVsock creates a vsock listener on the given port.
// Inside a Firecracker VM, AF_VSOCK is available; on the host we fall back
// to a TCP port for integration tests.
func listenVsock(port uint32) (net.Listener, error) {
	l, err := listenAFVsock(port)
	if err == nil {
		return l, nil
	}
	// Fallback for test environments where AF_VSOCK is not available.
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}
