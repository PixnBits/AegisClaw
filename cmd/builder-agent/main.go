package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"golang.org/x/sys/unix"
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

	if err := prepareRuntimeFS(logger); err != nil {
		logger.Fatal("failed to prepare runtime filesystem", zap.Error(err))
	}

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

// prepareRuntimeFS performs minimal PID1 setup for the builder VM.
// When running as init, we must mount /proc and attach the workspace disk.
func prepareRuntimeFS(logger *zap.Logger) error {
	_ = os.MkdirAll("/proc", 0755)
	_ = os.MkdirAll("/sys", 0755)
	_ = os.MkdirAll("/dev", 0755)
	_ = os.MkdirAll("/workspace", 0755)
	_ = os.MkdirAll("/var/lib/aegisclaw", 0755)

	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil && err != syscall.EBUSY {
		logger.Warn("failed to mount /proc", zap.Error(err))
	}
	if err := syscall.Mount("sysfs", "/sys", "sysfs", 0, ""); err != nil && err != syscall.EBUSY {
		logger.Warn("failed to mount /sys", zap.Error(err))
	}
	if err := syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, ""); err != nil && err != syscall.EBUSY {
		logger.Warn("failed to mount /dev", zap.Error(err))
	}

	if err := syscall.Mount("/dev/vdb", "/workspace", "ext4", 0, ""); err != nil && err != syscall.EBUSY {
		return fmt.Errorf("mount /dev/vdb -> /workspace: %w", err)
	}
	if err := os.MkdirAll("/workspace/tmp", 0777); err != nil {
		return fmt.Errorf("create /workspace/tmp: %w", err)
	}
	if err := os.MkdirAll("/workspace/.cache/go-build", 0755); err != nil {
		return fmt.Errorf("create /workspace/.cache/go-build: %w", err)
	}

	logger.Info("runtime filesystem prepared",
		zap.String("workspace", "/workspace"),
		zap.String("workspace_device", "/dev/vdb"),
	)
	return nil
}

// initKernel initializes the kernel for audit logging.
func initKernel(logger *zap.Logger) (*kernel.Kernel, error) {
	if err := os.Setenv("HOME", "/workspace"); err != nil {
		logger.Warn("failed to set HOME for builder agent", zap.Error(err))
	}
	if err := os.Setenv("PATH", "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"); err != nil {
		logger.Warn("failed to set PATH for builder agent", zap.Error(err))
	}
	if err := os.Setenv("TMPDIR", "/workspace/tmp"); err != nil {
		logger.Warn("failed to set TMPDIR for builder agent", zap.Error(err))
	}
	if err := os.Setenv("GOCACHE", "/workspace/.cache/go-build"); err != nil {
		logger.Warn("failed to set GOCACHE for builder agent", zap.Error(err))
	}
	// In the microVM, the kernel state is shared via vsock or a mounted volume
	// Use GetInstance with a local audit directory
	auditDir := os.Getenv("AUDIT_DIR")
	if auditDir == "" {
		auditDir = "/workspace/audit"
	}
	
	// Ensure audit directory exists
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit directory: %w", err)
	}
	
	return kernel.GetInstance(logger, auditDir)
}

// initProposalStore initializes the proposal store.
func initProposalStore(logger *zap.Logger) (*proposal.Store, error) {
	// Proposal store should be accessible via a mounted volume or vsock
	// For now, use a local directory
	storeDir := os.Getenv("PROPOSAL_STORE_DIR")
	if storeDir == "" {
		storeDir = "/workspace/proposals"
	}

	return proposal.NewStore(storeDir, logger)
}

// initPipeline initializes the builder pipeline.
// Since this code runs INSIDE a microVM, we use simplified in-VM components
// that don't spawn nested VMs.
func initPipeline(kern *kernel.Kernel, store *proposal.Store, logger *zap.Logger) (builder.PipelineExecutor, error) {
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

	// Initialize in-VM code generator
	// Ollama is available via vsock proxy on the host
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://127.0.0.1:11434" // Default Ollama endpoint via proxy
	}
	codeGen := NewInVMCodeGenerator(kern, logger, ollamaURL)

	// Initialize in-VM analyzer
	analyzer := NewInVMAnalyzer(kern, logger)

	// Create in-VM pipeline that runs everything directly without nested VMs
	pipeline, err := NewInVMPipeline(codeGen, analyzer, gitMgr, kern, store, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create in-VM pipeline: %w", err)
	}

	logger.Info("in-VM pipeline initialized successfully",
		zap.String("workspace", workspaceDir),
		zap.String("ollama_url", ollamaURL),
	)

	return pipeline, nil
}


// startVsockListener listens for build requests on vsock port 1024.
func startVsockListener(ctx context.Context, agent *builder.BuilderAgent, logger *zap.Logger) {
	// The virtio_vsock transport may not be ready when PID 1 starts.
	var (
		listener net.Listener
		err      error
	)
	for attempt := 0; attempt < 20; attempt++ {
		listener, err = listenVsock(1024)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if listener == nil {
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

type vsockConn struct {
	file *os.File
}

func (c *vsockConn) Read(b []byte) (int, error)         { return c.file.Read(b) }
func (c *vsockConn) Write(b []byte) (int, error)        { return c.file.Write(b) }
func (c *vsockConn) Close() error                       { return c.file.Close() }
func (c *vsockConn) LocalAddr() net.Addr                { return vsockAddr(0) }
func (c *vsockConn) RemoteAddr() net.Addr               { return vsockAddr(0) }
func (c *vsockConn) SetDeadline(t time.Time) error      { return c.file.SetDeadline(t) }
func (c *vsockConn) SetReadDeadline(t time.Time) error  { return c.file.SetReadDeadline(t) }
func (c *vsockConn) SetWriteDeadline(t time.Time) error { return c.file.SetWriteDeadline(t) }

type vsockListener struct {
	fd   int
	port int
}

func (l *vsockListener) Accept() (net.Conn, error) {
	nfd, _, err := unix.Accept(l.fd)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(nfd), "vsock-conn")
	return &vsockConn{file: file}, nil
}

func (l *vsockListener) Close() error   { return unix.Close(l.fd) }
func (l *vsockListener) Addr() net.Addr { return vsockAddr(l.port) }

type vsockAddr int

func (a vsockAddr) Network() string { return "vsock" }
func (a vsockAddr) String() string  { return fmt.Sprintf("vsock://:%d", int(a)) }

func listenVsock(port int) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket(AF_VSOCK): %w", err)
	}

	sa := &unix.SockaddrVM{
		CID:  unix.VMADDR_CID_ANY,
		Port: uint32(port),
	}
	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind vsock port %d: %w", port, err)
	}

	if err := unix.Listen(fd, 5); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("listen vsock: %w", err)
	}

	return &vsockListener{fd: fd, port: port}, nil
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
