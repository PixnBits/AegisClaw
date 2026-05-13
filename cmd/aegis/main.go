// Package main implements the AegisClaw Host Daemon.
// The daemon is responsible for starting, stopping, and monitoring sandboxed VMs.
// On Linux, VMs are Firecracker microVMs. On macOS/Windows, they're Docker Sandboxes.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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

	// If /proc doesn't work (non-Linux), try sending signal 0 to test if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	signalErr := process.Signal(syscall.Signal(0))
	if signalErr == nil {
		return true
	}

	// Check if error indicates process doesn't exist (vs permission error)
	errStr := signalErr.Error()
	return !strings.Contains(errStr, "no such process")
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

	// For now, just keep the daemon running
	// TODO: Add HTTP API server for starting/stopping VMs
	logrus.Info("daemon ready")

	// Block forever
	select {}
}

func stopDaemon(cmd *cobra.Command, args []string) {
	if os.Getuid() != 0 {
		fmt.Println("stop requires root privileges (use: sudo aegis stop)")
		os.Exit(1)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Println("daemon not running")
		removePIDFile() // Clean up if it exists
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

	// Try to terminate the process
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// If process doesn't exist, just clean up the PID file
		if strings.Contains(err.Error(), "no such process") {
			fmt.Println("daemon not running (PID file stale, cleaned up)")
			removePIDFile()
			return
		}
		fmt.Printf("failed to stop daemon: %v\n", err)
		return
	}

	// Wait for daemon to shut down gracefully (up to 10 seconds)
	for i := 0; i < 100; i++ {
		if isDaemonRunning() == false {
			fmt.Println("daemon stopped")
			removePIDFile()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// If still running, try SIGKILL
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

func listVMs(cmd *cobra.Command, args []string) {
	if !isDaemonRunning() {
		fmt.Println("daemon is not running")
		return
	}

	// TODO: Connect to daemon API and list VMs
	fmt.Println("VM listing not yet implemented")
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

	vmCmd.AddCommand(listCmd)
	rootCmd.AddCommand(startCmd, stopCmd, statusCmd, doctorCmd, vmCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
