// Package main implements the AegisClaw Host Daemon.
// The daemon is responsible for starting, stopping, and monitoring sandboxed VMs.
// On Linux, VMs are Firecracker microVMs. On macOS/Windows, they're Docker Sandboxes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"AegisClaw/internal/config"
	"AegisClaw/internal/runtime"
)

var (
	socketPath   string
	pidFile      string
	orchestrator *runtime.Orchestrator
	cfg          *config.Config
	jsonOutput   bool
)

func init() {
	cfg = config.New()

	// Use /tmp for the PID file so it's accessible to both root and non-root users
	// This avoids issues where sudo runs as root but status checks as regular user
	stateDir := filepath.Join("/tmp", "aegis")

	socketPath = filepath.Join(stateDir, "daemon.sock")
	pidFile = filepath.Join(stateDir, "daemon.pid")
}

func setupLogging() error {
	logDir := filepath.Join(os.Getenv("HOME"), ".aegis")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile := filepath.Join(logDir, "daemon.log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logrus.SetOutput(file)
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetLevel(logrus.InfoLevel)

	return nil
}

func ensureStateDir() error {
	stateDir := filepath.Join("/tmp", "aegis")

	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// For Linux, also ensure rootfs directory exists
	if cfg.SandboxType == config.Firecracker {
		if err := os.MkdirAll(cfg.RootfsDir, 0755); err != nil {
			return fmt.Errorf("failed to create rootfs directory: %w", err)
		}
	}

	return nil
}

func isDaemonRunning() bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}

	// Check /proc first (most reliable, works across privilege boundaries)
	procPath := fmt.Sprintf("/proc/%d", pid)
	if _, err := os.Stat(procPath); err == nil {
		return true
	}

	// If /proc check failed (non-Linux or process doesn't exist), clean up stale PID file
	// This is conservative: if we can't verify the process, assume it's stale
	_ = os.Remove(pidFile)
	return false
}

func writePIDFile() error {
	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(pidFile), 0777); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}

	// Write PID file with world-readable permissions so non-root can clean it up if stale
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0666); err != nil {
		return err
	}

	// Make directory writable by all so PID file can be cleaned up
	return os.Chmod(filepath.Dir(pidFile), 0777)
}

func removePIDFile() {
	_ = os.Remove(pidFile)
}

func startDaemon(cmd *cobra.Command, args []string) {
	if os.Getuid() != 0 {
		fmt.Println("daemon must be started with root privileges (use: sudo aegis start)")
		os.Exit(1)
	}

	// Check if already running
	if isDaemonRunning() {
		fmt.Println("daemon already running")
		return
	}

	foreground, _ := cmd.Flags().GetBool("foreground")

	// Fork to background if not in foreground mode
	if !foreground {
		daemonCmd := exec.Command(os.Args[0], "start", "--foreground")

		// Set Setsid on Unix-like platforms for process group isolation
		setSetsid(daemonCmd)

		if err := daemonCmd.Start(); err != nil {
			fmt.Printf("failed to start daemon: %v\n", err)
			os.Exit(1)
		}

		// Wait for PID file to be written (signals daemon is ready)
		for i := 0; i < 30; i++ {
			if _, err := os.Stat(pidFile); err == nil {
				fmt.Println("daemon started")
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Println("timeout waiting for daemon to start")
		os.Exit(1)
	}

	// Setup logging
	if err := setupLogging(); err != nil {
		fmt.Printf("failed to setup logging: %v\n", err)
		os.Exit(1)
	}

	// Ensure state directory
	if err := ensureStateDir(); err != nil {
		logrus.Fatalf("failed to ensure state directory: %v", err)
	}

	// Create orchestrator
	var err error
	orchestrator, err = runtime.New(cfg)
	if err != nil {
		logrus.Fatalf("failed to create orchestrator: %v", err)
	}

	logrus.Infof("daemon starting on platform %s with sandbox type %s",
		cfg.Platform, cfg.SandboxType)

	// Write PID file
	if err := writePIDFile(); err != nil {
		logrus.Fatalf("failed to write PID file: %v", err)
	}
	defer removePIDFile()

	// Start socket server
	if err := startSocketServer(socketPath, orchestrator); err != nil {
		logrus.Fatalf("failed to start socket server: %v", err)
	}

	// Phase 5: Minimal hardened reverse proxy for Web Portal (per web-portal-vm.md + host-daemon.md)
	// The Web Portal must receive traffic ONLY through the Host Daemon.
	// We start the portal on an internal address and proxy from the public :8080.
	go func() {
		if err := startManagedWebPortal(); err != nil {
			logrus.Errorf("web-portal management: %v", err)
		}
	}()

	// Start the public-facing reverse proxy (users hit this at http://localhost:8080)
	// This is the only inbound path to the Web Portal.
	if err := startWebPortalProxy("127.0.0.1:8080", "http://127.0.0.1:18080"); err != nil {
		logrus.Fatalf("failed to start web portal reverse proxy: %v", err)
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logrus.Info("shutting down daemon")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := orchestrator.Shutdown(ctx); err != nil {
			logrus.Errorf("error during shutdown: %v", err)
		}
		os.Exit(0)
	}()

	logrus.Info("daemon ready")

	// Demo: sign a genesis audit root using the new TCB signing path (exercises
	// per-daemon key + audit responsibility). Real Merkle roots will be signed
	// on events and periodically.
	if sig, err := orchestrator.SignAuditRoot([]byte("genesis-audit-root")); err == nil {
		logrus.Infof("genesis audit root signed (len=%d)", len(sig))
	}
}

func stopDaemon(cmd *cobra.Command, args []string) {
	// Prefer socket-based stop: this allows non-root users to stop a root-started
	// daemon (per AGENTS.md and docs/specs/cli.md). The root daemon honors the
	// "stop" command over the (currently open) socket and performs its own shutdown.
	socket := expandPath(socketPath)
	if conn, err := net.Dial("unix", socket); err == nil {
		defer conn.Close()
		if _, err := conn.Write([]byte("stop")); err == nil {
			buf := make([]byte, 256)
			if n, err := conn.Read(buf); err == nil {
				resp := strings.TrimSpace(string(buf[:n]))
				fmt.Printf("%s\n", resp)
			}
			// Give the daemon a moment to exit and clean PID
			for i := 0; i < 50; i++ {
				if !isDaemonRunning() {
					fmt.Println("daemon stopped (via socket)")
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			fmt.Println("daemon stop requested via socket (may still be shutting down)")
			return
		}
	}

	// Fallback: direct PID signal (works when daemon not root-owned or same-user)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Println("daemon not running")
		removePIDFile()
		return
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		fmt.Println("failed to parse PID")
		removePIDFile()
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("daemon not running")
		removePIDFile()
		return
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		if strings.Contains(err.Error(), "no such process") {
			fmt.Println("daemon not running (PID file stale, cleaned up)")
			removePIDFile()
			return
		}
		fmt.Printf("failed to stop daemon via signal: %v (try with sudo if daemon is root-owned)\n", err)
		return
	}

	for i := 0; i < 100; i++ {
		if !isDaemonRunning() {
			fmt.Println("daemon stopped")
			removePIDFile()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	process.Signal(syscall.SIGKILL)
	time.Sleep(500 * time.Millisecond)
	fmt.Println("daemon stopped (forced)")
	removePIDFile()
}

func statusDaemon(cmd *cobra.Command, args []string) {
	if isDaemonRunning() {
		fmt.Println("daemon is running")
		return
	}
	fmt.Println("daemon is not running")
}

func doctorDaemon(cmd *cobra.Command, args []string) {
	fmt.Println("Running health checks...")

	// Check if running as root
	if os.Getuid() != 0 {
		fmt.Println("⚠ Not running as root (required for daemon)")
	} else {
		fmt.Println("✓ Running as root")
	}

	// Check platform
	fmt.Printf("✓ Platform: %s\n", cfg.Platform)
	fmt.Printf("✓ Sandbox type: %s\n", cfg.SandboxType)

	// Check state directory
	if err := ensureStateDir(); err != nil {
		fmt.Printf("✗ State directory check failed: %v\n", err)
	} else {
		fmt.Printf("✓ State directory: %s\n", cfg.StateDir)
	}

	// Check if daemon is running
	if isDaemonRunning() {
		fmt.Println("✓ Daemon is running")
	} else {
		fmt.Println("⚠ Daemon is not running")
	}

	fmt.Println("\nHealth checks complete")
}

func getOriginalUser() (*user.User, error) {
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser != "" {
		return user.Lookup(sudoUser)
	}
	// If not sudo, return current user
	return user.Current()
}

func expandPath(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		origUser, err := getOriginalUser()
		if err != nil {
			// fallback to current user's home
			home, _ := os.UserHomeDir()
			return filepath.Join(home, path[2:])
		}
		return filepath.Join(origUser.HomeDir, path[2:])
	}
	return path
}

func startSocketServer(socketPath string, orch *runtime.Orchestrator) error {
	socket := expandPath(socketPath)
	dir := filepath.Dir(socket)

	// Create socket directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove old socket if it exists
	os.Remove(socket)

	listener, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Harden socket: chown to the original invoking user (from sudo or current)
	// and chmod 0600 so only that user (and root) can connect. This is a key
	// TCB hardening step per host-daemon.md (strict permissions + input validation).
	if u, err := getOriginalUser(); err == nil {
		if uid, perr := strconv.Atoi(u.Uid); perr == nil {
			gid := uid
			if g, gerr := strconv.Atoi(u.Gid); gerr == nil {
				gid = g
			}
			_ = os.Chown(socket, uid, gid)
		}
	}
	if err := os.Chmod(socket, 0600); err != nil {
		// Fallback to world for unusual setups (still better than before in most cases)
		_ = os.Chmod(socket, 0666)
	}

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				logrus.Errorf("socket accept error: %v", err)
				continue
			}
			go handleSocketCommand(conn, orch)
		}
	}()

	logrus.Infof("socket server listening on %s", socket)
	return nil
}

func handleSocketCommand(conn net.Conn, orch *runtime.Orchestrator) {
	defer conn.Close()

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		logrus.Errorf("socket read error: %v", err)
		return
	}

	command := strings.TrimSpace(string(buf[:n]))
	logrus.Debugf("received command: %s", command)

	// Input validation / hardening (per host-daemon.md Unix Socket Hardening req)
	if len(command) == 0 || len(command) > 64 {
		conn.Write([]byte("invalid command\n"))
		return
	}
	// Allowlist (extend as we add commands in later phases)
	allowed := map[string]bool{"vm list": true, "stop": true}
	if !allowed[command] {
		logrus.Warnf("unauthorized or unknown socket command: %s", command)
		conn.Write([]byte("unauthorized\n"))
		return
	}

	response := ""
	switch command {
	case "vm list":
		vms, err := orch.ListVMs(context.Background())
		if err != nil {
			response = fmt.Sprintf("Error listing VMs: %v\n", err)
		} else if len(vms) == 0 {
			response = "No running VMs\n"
		} else {
			for _, vm := range vms {
				response += fmt.Sprintf("%s: %s (%s)\n", vm.ID, vm.Type, vm.Status)
			}
		}
	case "stop":
		response = "stopping\n"
		conn.Write([]byte(response))
		logrus.Info("stop command received via socket - initiating graceful shutdown")
		go func() {
			if orchestrator != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := orchestrator.Shutdown(ctx); err != nil {
					logrus.Errorf("shutdown error during socket stop: %v", err)
				}
			}
			removePIDFile()
			os.Exit(0)
		}()
		return
	default:
		response = "Unknown command\n"
	}

	conn.Write([]byte(response))
}

func listVMs(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte("vm list"))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading response")
		return
	}
	response := string(buf[:n])
	if jsonOutput {
		lines := strings.Split(strings.TrimSpace(response), "\n")
		vms := []map[string]string{}
		for _, line := range lines {
			if line == "No running VMs" || line == "" {
				break
			}
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) >= 2 {
				vms = append(vms, map[string]string{"id": parts[0], "type": strings.Split(parts[1], " ")[0]})
			}
		}
		jsonBytes, _ := json.Marshal(vms)
		fmt.Println(string(jsonBytes))
	} else {
		fmt.Printf("Running VMs:\n%s", response)
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "aegis",
		Short: "AegisClaw Host Daemon",
		Long: "The Host Daemon manages sandboxed VMs for AegisClaw components." +
			"\nOn Linux, uses Firecracker microVMs. On macOS/Windows, uses Docker Sandboxes.",
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon",
		Run:   startDaemon,
	}
	startCmd.Flags().Bool("foreground", false, "Run daemon in foreground")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		Run:   stopDaemon,
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		Run:   statusDaemon,
	}

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks",
		Run:   doctorDaemon,
	}

	vmCmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage VMs",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List running VMs",
		Run:   listVMs,
	}
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	vmCmd.AddCommand(listCmd)
	rootCmd.AddCommand(startCmd, stopCmd, statusCmd, doctorCmd, vmCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// startManagedWebPortal starts the web-portal binary as a managed child process
// on an internal address (127.0.0.1:18080). This is part of the Host Daemon's
// responsibility to mediate all access to the Web Portal (per web-portal-vm.md).
func startManagedWebPortal() error {
	webPortalBinary := "./bin/web-portal"
	if _, err := os.Stat(webPortalBinary); os.IsNotExist(err) {
		// Fall back to looking in PATH or same dir as daemon (useful in dev)
		webPortalBinary = "web-portal"
	}

	cmd := exec.Command(webPortalBinary)
	cmd.Env = append(os.Environ(),
		"AEGIS_WEB_PORTAL_LISTEN_ADDR=127.0.0.1:18080",
		// In real deployment this would also pass vsock or hub socket info
	)

	// Inherit logging from the daemon for now (goes to ~/.aegis/daemon.log)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logrus.Info("starting managed web-portal on internal address 127.0.0.1:18080")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start web-portal: %w", err)
	}

	// In a more complete implementation we would track this in the orchestrator
	// and restart on crash. For Phase 5 minimal proxy this is sufficient.
	go func() {
		if err := cmd.Wait(); err != nil {
			logrus.Warnf("managed web-portal exited: %v", err)
		}
	}()

	return nil
}

// startWebPortalProxy starts a minimal, hardened reverse proxy on the public
// address (typically 127.0.0.1:8080) that forwards to the internal web-portal.
// This is the ONLY way users should reach the Web Portal.
func startWebPortalProxy(listenAddr, targetURL string) error {
	target, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid web portal target: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// Hardening (per host-daemon.md TCB requirements and plan notes)
	proxy.Transport = &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	// Wrap with basic hardening middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple request size limit (protect against huge uploads)
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB

		// Log high-level access (goes to daemon log / audit trail)
		logrus.Infof("web-proxy: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		proxy.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second, // longer for SSE/streaming chat
		IdleTimeout:  120 * time.Second,
	}

	logrus.Infof("web portal reverse proxy listening on %s (forwarding to %s)", listenAddr, targetURL)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("web portal proxy error: %v", err)
		}
	}()

	return nil
}
