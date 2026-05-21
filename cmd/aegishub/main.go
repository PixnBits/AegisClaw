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

// AegisHub is the system IPC router microVM for AegisClaw.
// It routes messages between the daemon, the Governance Court, and the Store VM.
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

// HubResponse is the standard JSON envelope for all AegisHub messages.
type HubResponse struct {
	ID      string      `json:"id"`
	Success bool        `json:"success,omitempty"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func (s *server) handleProposalList(req *ipc.Message) *HubResponse {
	summaries, err := s.hub.Router().RegisteredRoutes()
	if err != nil {
		return errResponse(req.ID, "failed to list routes: "+err.Error())
	}
	data, _ := json.Marshal(summaries)
	return &HubResponse{ID: req.ID, Success: true, Data: data}
}

func (s *server) handleProposalStatus(req *ipc.Message) *HubResponse {
	var payload struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errResponse(req.ID, "invalid payload: "+err.Error())
	}
	// Delegate to the registered store-vm backend via the hub router
	result, err := s.hub.Router().Route("proposal.status", req)
	if err != nil {
		return errResponse(req.ID, "route failed: "+err.Error())
	}
	if result.Error != "" {
		return &HubResponse{ID: req.ID, Success: false, Error: result.Error}
	}
	return &HubResponse{ID: req.ID, Success: true, Data: result.Response}
}

func (s *server) handleMemoryRetrieve(req *ipc.Message) *HubResponse {
	// Placeholder for memory retrieval logic
	return &HubResponse{ID: req.ID, Success: true, Data: map[string]interface{}{}}
}

func (s *server) handleCompositionCurrent(req *ipc.Message) *HubResponse {
	// Placeholder for composition manifest retrieval
	return &HubResponse{ID: req.ID, Success: true, Data: map[string]interface{}{}}
}

func (s *server) sendErr(enc *json.Encoder, id, msg string) {
	resp := &HubResponse{ID: id, Error: msg}
	enc.Encode(resp) //nolint:errcheck
}

func errResponse(id, msg string) *HubResponse {
	return &HubResponse{ID: id, Error: msg}
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
