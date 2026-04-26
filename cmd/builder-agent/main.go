package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/builder"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("builder agent starting")

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// Initialize kernel for audit logging
	kern, err := initKernel(logger)
	if err != nil {
		logger.Fatal("failed to initialize kernel", zap.Error(err))
	}

	// Initialize proposal store
	store, err := initProposalStore(logger)
	if err != nil {
		logger.Fatal("failed to initialize proposal store", zap.Error(err))
	}

	// Initialize pipeline
	pipeline, err := initPipeline(kern, store, logger)
	if err != nil {
		logger.Fatal("failed to initialize pipeline", zap.Error(err))
	}

	// Create builder agent
	agent := builder.NewBuilderAgent(pipeline, store, kern, logger)

	// Start vsock listener in background
	go startVsockListener(ctx, agent, logger)

	// Run agent main loop
	if err := agent.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("agent error", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("builder agent stopped")
}

// initKernel initializes the kernel for audit logging.
func initKernel(logger *zap.Logger) (*kernel.Kernel, error) {
	// In the microVM, use an in-memory kernel without persistent audit
	// The host daemon has the main audit trail
	kern, err := kernel.GetInstance(logger, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create kernel: %w", err)
	}
	return kern, nil
}

// initProposalStore initializes the proposal store.
func initProposalStore(logger *zap.Logger) (*proposal.Store, error) {
	// Proposal store should be accessible via a mounted volume or vsock
	// For now, use a local directory
	storeDir := os.Getenv("PROPOSAL_STORE_DIR")
	if storeDir == "" {
		storeDir = "/var/lib/aegisclaw/proposals"
	}

	return proposal.NewStore(storeDir, logger)
}

// initPipeline initializes the builder pipeline for in-process execution.
// When running inside the builder microVM, we don't need to launch nested VMs.
// Instead, we run code generation and analysis directly in this VM.
func initPipeline(kern *kernel.Kernel, store *proposal.Store, logger *zap.Logger) (*builder.Pipeline, error) {
	// Get configuration from environment
	workspaceDir := os.Getenv("WORKSPACE_DIR")
	if workspaceDir == "" {
		workspaceDir = "/workspace"
	}

	// Initialize git manager
	gitMgr, err := gitmanager.NewManager(workspaceDir, kern, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create git manager: %w", err)
	}

	// Create in-process builder runtime (stub that doesn't launch nested VMs)
	// We're already running inside the builder VM, so we execute directly.
	builderRT := builder.NewInProcessBuilderRuntime(logger)

	// Create code generator with direct Ollama HTTP client
	// Inside the microVM, Ollama is accessible at localhost:11434 via the LLM proxy
	templates := builder.DefaultPromptTemplates()
	codeGen, err := builder.NewCodeGenerator(builderRT, kern, logger, templates)
	if err != nil {
		return nil, fmt.Errorf("failed to create code generator: %w", err)
	}

	// Create analyzer for running tests and linting directly
	analyzer, err := builder.NewAnalyzer(builderRT, kern, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create analyzer: %w", err)
	}

	// Create pipeline with all components
	pipeline, err := builder.NewPipeline(builderRT, codeGen, gitMgr, analyzer, kern, store, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}

	logger.Info("pipeline initialized for in-process execution")
	return pipeline, nil
}

// startVsockListener listens for build requests on vsock port 1024.
func startVsockListener(ctx context.Context, agent *builder.BuilderAgent, logger *zap.Logger) {
	// Vsock listener on port 1024 (Firecracker vsock convention)
	listener, err := net.Listen("vsock", ":1024")
	if err != nil {
		logger.Error("failed to start vsock listener", zap.Error(err))
		return
	}
	defer listener.Close()

	logger.Info("vsock listener started on port 1024")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := listener.Accept()
		if err != nil {
			logger.Error("failed to accept vsock connection", zap.Error(err))
			continue
		}

		go handleVsockConnection(ctx, conn, agent, logger)
	}
}

// handleVsockConnection handles a single vsock connection.
func handleVsockConnection(ctx context.Context, conn net.Conn, agent *builder.BuilderAgent, logger *zap.Logger) {
	defer conn.Close()

	// Read request
	decoder := json.NewDecoder(conn)
	var req kernel.ControlMessage
	if err := decoder.Decode(&req); err != nil {
		logger.Error("failed to decode vsock request", zap.Error(err))
		return
	}

	logger.Info("received vsock message", zap.String("type", req.Type))

	// Handle build request
	if req.Type == "builder.execute" {
		var buildReq builder.BuildRequest
		if err := json.Unmarshal(req.Payload, &buildReq); err != nil {
			logger.Error("failed to unmarshal build request", zap.Error(err))
			return
		}

		// Process request
		resp, err := agent.HandleBuildRequest(ctx, &buildReq)
		if err != nil {
			logger.Error("failed to handle build request", zap.Error(err))
			return
		}

		// Send response
		encoder := json.NewEncoder(conn)
		if err := encoder.Encode(resp); err != nil {
			logger.Error("failed to encode build response", zap.Error(err))
		}
	}
}
