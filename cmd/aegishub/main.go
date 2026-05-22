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

func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()
	// TODO: Implement message framing and routing logic here.
	// For now, AegisHub acts as a pass-through to the registered skills.
	s.logger.Debug("aegishub: new connection accepted")
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
