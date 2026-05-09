package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var socketPath = "~/.aegis/daemon.sock"

type VMConfig struct {
	ID         string
	Image      string
	KernelPath string
	RootfsPath string
}

type SandboxBackend interface {
	StartVM(ctx context.Context, config VMConfig) error
	StopVM(ctx context.Context, id string) error
	StatusVM(ctx context.Context, id string) (string, error)
}

type DockerBackend struct{}

func NewDockerBackend() *DockerBackend {
	return &DockerBackend{}
}

func (d *DockerBackend) StartVM(ctx context.Context, config VMConfig) error {
	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--name", config.ID, config.Image, "sleep", "infinity")
	return cmd.Run()
}

func (d *DockerBackend) StopVM(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", id)
	return cmd.Run()
}

func (d *DockerBackend) StatusVM(ctx context.Context, id string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", id)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

type FirecrackerBackend struct{}

func (f *FirecrackerBackend) StartVM(ctx context.Context, config VMConfig) error {
	// Stub implementation
	log.Printf("Starting Firecracker VM %s", config.ID)
	return nil
}

func (f *FirecrackerBackend) StopVM(ctx context.Context, id string) error {
	log.Printf("Stopping Firecracker VM %s", id)
	return nil
}

func (f *FirecrackerBackend) StatusVM(ctx context.Context, id string) (string, error) {
	return "running", nil
}

var backend SandboxBackend
var runningVMs sync.Map

func initBackend() {
	if runtime.GOOS == "linux" {
		backend = &FirecrackerBackend{}
	} else {
		backend = NewDockerBackend()
	}
}

func setupLogging() {
	logFile := expandPath("~/.aegis/daemon.log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		logrus.SetOutput(file)
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func startDaemon(cmd *cobra.Command, args []string) {
	if os.Getuid() != 0 {
		fmt.Println("Host Daemon must be started with root privileges")
		os.Exit(1)
	}

	socket := expandPath(socketPath)
	dir := filepath.Dir(socket)
	os.MkdirAll(dir, 0700)
	os.Remove(socket) // Remove existing socket if any

	listener, err := net.Listen("unix", socket)
	if err != nil {
		fmt.Printf("Failed to start daemon: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("AegisClaw daemon started. Listening on", socket)

	done := make(chan bool)
	// For now, just accept connections
	for {
		select {
		case <-done:
			fmt.Println("Daemon stopping...")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				fmt.Printf("Accept error: %v\n", err)
				continue
			}
			go handleConnection(conn, done)
		}
	}
}

func handleConnection(conn net.Conn, done chan bool) {
	defer conn.Close()
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		traceID := uuid.New().String()
		logger := logrus.WithField("trace_id", traceID)
		parts := strings.Fields(string(buf[:n]))
		if len(parts) == 0 {
			logger.Warn("Empty command received")
			conn.Write([]byte("empty command\n"))
			continue
		}
		cmd := parts[0]
		logger.WithField("command", cmd).Info("Handling command")
		switch cmd {
		case "status":
			logger.Info("Daemon status requested")
			conn.Write([]byte("running\n"))
		case "stop":
			logger.Info("Stopping daemon")
			conn.Write([]byte("stopping\n"))
			done <- true
			return
		case "start-vm":
			if len(parts) < 3 {
				logger.Warn("Invalid start-vm command")
				conn.Write([]byte("usage: start-vm <id> <image>\n"))
				continue
			}
			id := parts[1]
			image := parts[2]
			logger.WithFields(logrus.Fields{"vm_id": id, "image": image}).Info("Starting VM")
			config := VMConfig{ID: id, Image: image}
			err := backend.StartVM(context.Background(), config)
			if err != nil {
				logger.WithError(err).Error("Failed to start VM")
				conn.Write([]byte("error: " + err.Error() + "\n"))
			} else {
				runningVMs.Store(id, config)
				conn.Write([]byte("started\n"))
			}
		case "stop-vm":
			if len(parts) < 2 {
				logger.Warn("Invalid stop-vm command")
				conn.Write([]byte("usage: stop-vm <id>\n"))
				continue
			}
			id := parts[1]
			logger.WithField("vm_id", id).Info("Stopping VM")
			err := backend.StopVM(context.Background(), id)
			if err != nil {
				logger.WithError(err).Error("Failed to stop VM")
				conn.Write([]byte("error: " + err.Error() + "\n"))
			} else {
				conn.Write([]byte("stopped\n"))
			}
		case "status-vm":
			if len(parts) < 2 {
				logger.Warn("Invalid status-vm command")
				conn.Write([]byte("usage: status-vm <id>\n"))
				continue
			}
			id := parts[1]
			logger.WithField("vm_id", id).Info("VM status requested")
			status, err := backend.StatusVM(context.Background(), id)
			if err != nil {
				logger.WithError(err).Error("Failed to get VM status")
				conn.Write([]byte("error: " + err.Error() + "\n"))
			} else {
				conn.Write([]byte(status + "\n"))
			}
		case "safe-mode":
			if len(parts) < 2 {
				logger.Warn("Invalid safe-mode command")
				conn.Write([]byte("usage: safe-mode <enable|disable>\n"))
				continue
			}
			action := parts[1]
			if action == "enable" {
				logger.Info("Enabling safe mode")
				runningVMs.Range(func(key, value interface{}) bool {
					id := key.(string)
					backend.StopVM(context.Background(), id)
					runningVMs.Delete(id)
					return true
				})
				conn.Write([]byte("safe-mode enabled\n"))
			} else if action == "disable" {
				logger.Info("Disabling safe mode")
				conn.Write([]byte("safe-mode disabled\n"))
			} else {
				logger.Warn("Unknown safe-mode action")
				conn.Write([]byte("unknown action\n"))
			}
		default:
			conn.Write([]byte("unknown command\n"))
		}
	}
}

func enableSafeMode(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte("safe-mode enable\n"))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("Safe mode: %s", string(buf[:n]))
}

func showLogs(cmd *cobra.Command, args []string) {
	logFile := expandPath("~/.aegis/daemon.log")
	exec.Command("tail", "-f", logFile).Run()
}

func disableSafeMode(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte("safe-mode disable\n"))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("Safe mode: %s", string(buf[:n]))
}

func stopDaemon(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte("stop"))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("Daemon: %s\n", string(buf[:n]))
}

func statusDaemon(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte("status"))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("Daemon status: %s\n", string(buf[:n]))
}

func doctorDaemon(cmd *cobra.Command, args []string) {
	fmt.Println("Running aegis doctor...")

	// Check if daemon is running
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("FAIL: Daemon not running")
		os.Exit(1)
	}
	conn.Close()

	fmt.Println("PASS: Daemon is running")

	// Check socket permissions
	info, err := os.Stat(filepath.Dir(socket))
	if err != nil || info.Mode().Perm() != 0700 {
		fmt.Println("FAIL: Socket directory permissions incorrect")
		os.Exit(1)
	}

	fmt.Println("PASS: Socket permissions correct")

	fmt.Println("All systems operational")
}

func main() {
	initBackend()
	setupLogging()
	if envSocket := os.Getenv("AEGIS_SOCKET"); envSocket != "" {
		socketPath = envSocket
	}

	var rootCmd = &cobra.Command{Use: "aegis"}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the AegisClaw daemon",
		Run:   startDaemon,
	}

	var statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		Run:   statusDaemon,
	}

	var doctorCmd = &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks",
		Run:   doctorDaemon,
	}

	var stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the AegisClaw daemon",
		Run:   stopDaemon,
	}

	var safeModeCmd = &cobra.Command{
		Use:   "safe-mode",
		Short: "Manage safe mode",
	}

	var enableCmd = &cobra.Command{
		Use:   "enable",
		Short: "Enable safe mode",
		Run:   enableSafeMode,
	}

	var disableCmd = &cobra.Command{
		Use:   "disable",
		Short: "Disable safe mode",
		Run:   disableSafeMode,
	}

	var logsCmd = &cobra.Command{
		Use:   "logs",
		Short: "Show daemon logs",
		Run:   showLogs,
	}

	safeModeCmd.AddCommand(enableCmd, disableCmd)

	rootCmd.AddCommand(startCmd, statusCmd, doctorCmd, stopCmd, safeModeCmd, logsCmd)
	rootCmd.Execute()
}