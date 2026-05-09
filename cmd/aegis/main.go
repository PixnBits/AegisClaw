package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var socketPath = "~/.aegis/daemon.sock"

var defaultKernelPath string
var defaultRootfsPath string

var startTime time.Time
var safeMode bool
var runningCmds sync.Map
var daemonPrivateKey ed25519.PrivateKey
var daemonPublicKey ed25519.PublicKey

type VMConfig struct {
	ID         string
	Image      string
	KernelPath string
	RootfsPath string
	StartTime  time.Time
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

func sendAPIRequest(sockPath, method, path string, body interface{}) error {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(jsonBody)
	}
	req, err := http.NewRequest(method, "http://localhost"+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}
	return nil
}

type FirecrackerBackend struct{}

func (f *FirecrackerBackend) StartVM(ctx context.Context, config VMConfig) error {
	if config.KernelPath == "" || config.RootfsPath == "" {
		return fmt.Errorf("KernelPath and RootfsPath required for Firecracker")
	}
	sockPath := "/tmp/firecracker-" + config.ID + ".sock"
	configPath := "/tmp/config-" + config.ID + ".json"
	configData := map[string]interface{}{
		"boot-source": map[string]interface{}{
			"kernel_image_path": config.KernelPath,
			"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off",
		},
		"drives": []map[string]interface{}{
			{
				"drive_id":       "rootfs",
				"path_on_host":   config.RootfsPath,
				"is_root_device": true,
				"is_read_only":   false,
			},
		},
	}
	configBytes, _ := json.Marshal(configData)
	err := os.WriteFile(configPath, configBytes, 0644)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "firecracker", "--api-sock", sockPath, "--config-file", configPath)
	err = cmd.Start()
	if err != nil {
		return err
	}
	runningCmds.Store(config.ID, cmd)
	// Wait for firecracker to be ready
	time.Sleep(500 * time.Millisecond)
	// Send start action
	err = sendAPIRequest(sockPath, "PUT", "/actions", map[string]string{"action_type": "InstanceStart"})
	if err != nil {
		cmd.Process.Kill()
		runningCmds.Delete(config.ID)
		return err
	}
	return nil
}

func (f *FirecrackerBackend) StopVM(ctx context.Context, id string) error {
	sockPath := "/tmp/firecracker-" + id + ".sock"
	// Send halt action
	sendAPIRequest(sockPath, "PUT", "/actions", map[string]string{"action_type": "InstanceHalt"})
	if cmd, ok := runningCmds.Load(id); ok {
		c := cmd.(*exec.Cmd)
		c.Process.Kill()
		runningCmds.Delete(id)
		runningVMs.Delete(id)
	}
	return nil
}

func (f *FirecrackerBackend) StatusVM(ctx context.Context, id string) (string, error) {
	if cmd, ok := runningCmds.Load(id); ok {
		c := cmd.(*exec.Cmd)
		if c.Process != nil {
			err := c.Process.Signal(syscall.Signal(0))
			if err == nil {
				return "running", nil
			}
		}
	}
	return "stopped", nil
}

var backend SandboxBackend
var runningVMs sync.Map
var jsonOutput bool
var foreground bool

func isDaemonRunning() bool {
	// Check socket first - this is the most reliable indicator
	socket := expandPath(socketPath)
	conn, err := net.DialTimeout("unix", socket, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()

	// Also check PID file for additional verification
	pidFile := expandPath("~/.aegis/daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}
	cmd := exec.Command("kill", "-0", strconv.Itoa(pid))
	return cmd.Run() == nil
}

func initBackend() {
	if runtime.GOOS == "linux" {
		backend = &FirecrackerBackend{}
	} else {
		backend = NewDockerBackend()
	}
}

func setupLogging() {
	logFile := expandPath("~/.aegis/daemon.log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err == nil {
		logrus.SetOutput(file)
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func expandPath(path string) string {
	if path[:2] == "~/" {
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

func getOriginalUser() (*user.User, error) {
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser != "" {
		return user.Lookup(sudoUser)
	}
	// If not sudo, return current user
	return user.Current()
}

func startDaemon(cmd *cobra.Command, args []string) {
	if os.Getuid() != 0 {
		fmt.Println("Host Daemon must be started with root privileges")
		os.Exit(1)
	}

	socket := expandPath(socketPath)

	// Check if daemon is already running
	if isDaemonRunning() {
		fmt.Println("Daemon already running")
		return
	}

	if !foreground {
		// Start daemon in background
		cmd := exec.Command(os.Args[0], "start", "--foreground")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		err := cmd.Start()
		if err != nil {
			fmt.Printf("Failed to start daemon: %v\n", err)
			return
		}
		// Wait for daemon to be ready by checking socket
		socket := expandPath(socketPath)
		timeout := time.After(10 * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				fmt.Println("Timeout waiting for daemon to start")
				return
			case <-ticker.C:
				if conn, err := net.Dial("unix", socket); err == nil {
					conn.Close()
					fmt.Println("Daemon started in background")
					return
				}
			}
		}
	}

	// Foreground: run the daemon
	dir := filepath.Dir(socket)
	os.MkdirAll(dir, 0700)

	// Get original user and chown directory to allow non-root access
	origUser, err := getOriginalUser()
	if err != nil {
		fmt.Printf("Failed to get original user: %v\n", err)
		os.Exit(1)
	}
	uid, _ := strconv.Atoi(origUser.Uid)
	gid, _ := strconv.Atoi(origUser.Gid)
	os.Chown(dir, uid, gid)

	// Set default image paths
	imagesDir := filepath.Join(origUser.HomeDir, ".aegis", "images")
	os.MkdirAll(imagesDir, 0700)
	os.Chown(imagesDir, uid, gid)
	defaultKernelPath = filepath.Join(imagesDir, "vmlinuz")
	defaultRootfsPath = filepath.Join(imagesDir, "rootfs.img")

	os.Remove(socket) // Remove existing socket if any

	listener, err := net.Listen("unix", socket)
	if err != nil {
		fmt.Printf("Failed to start daemon: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	// Chown socket to original user for non-root access
	os.Chown(socket, uid, gid)
	os.Chmod(socket, 0600)

	fmt.Println("AegisClaw daemon started. Listening on", socket)

	pidFile := expandPath("~/.aegis/daemon.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0600)
	os.Chown(pidFile, uid, gid)
	logFile := expandPath("~/.aegis/daemon.log")
	os.Chown(logFile, uid, gid)
	os.Chmod(logFile, 0600)

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
		case "vm":
			if len(parts) > 1 && parts[1] == "list" {
				logger.Info("VM list requested")
				var list []string
				runningVMs.Range(func(key, value interface{}) bool {
					id := key.(string)
					config := value.(VMConfig)
					status, _ := backend.StatusVM(context.Background(), id)
					image := config.Image
					if image == "" {
						image = fmt.Sprintf("kernel:%s rootfs:%s", config.KernelPath, config.RootfsPath)
					}
					uptime := time.Since(config.StartTime).Round(time.Second)
					list = append(list, fmt.Sprintf("%s: %s (%s, uptime %v)", id, image, status, uptime))
					return true
				})
				response := "No running VMs\n"
				if len(list) > 0 {
					response = strings.Join(list, "\n") + "\n"
				}
				conn.Write([]byte(response))
			} else {
				conn.Write([]byte("unknown vm command\n"))
			}
		case "status":
			logger.Info("Daemon status requested")
			count := 0
			runningVMs.Range(func(key, value interface{}) bool {
				count++
				return true
			})
			backendName := "Docker"
			if _, ok := backend.(*FirecrackerBackend); ok {
				backendName = "Firecracker"
			}
			response := fmt.Sprintf("Daemon: running\nBackend: %s\nSafe Mode: %t\nRunning VMs: %d\nUptime: %v\nPID: %d\n", backendName, safeMode, count, time.Since(startTime).Round(time.Second), os.Getpid())
			conn.Write([]byte(response))
		case "stop":
			logger.Info("Stopping daemon")
			// Gracefully stop all running VMs
			runningVMs.Range(func(key, value interface{}) bool {
				id := key.(string)
				logger.WithField("vm_id", id).Info("Stopping VM during daemon shutdown")
				backend.StopVM(context.Background(), id)
				runningVMs.Delete(id)
				return true
			})
			// Remove PID file and socket
			pidFile := expandPath("~/.aegis/daemon.pid")
			os.Remove(pidFile)
			socket := expandPath(socketPath)
			os.Remove(socket)
			conn.Write([]byte("stopping\n"))
			done <- true
			return
		case "start-vm":
			if len(parts) < 2 {
				logger.Warn("Invalid start-vm command")
				conn.Write([]byte("usage: start-vm <id> [kernel rootfs]\n"))
				continue
			}
			id := parts[1]
			config := VMConfig{ID: id, StartTime: time.Now()}
			if len(parts) == 2 {
				config.KernelPath = defaultKernelPath
				config.RootfsPath = defaultRootfsPath
			} else if len(parts) == 4 {
				config.KernelPath = parts[2]
				config.RootfsPath = parts[3]
			} else {
				conn.Write([]byte("usage: start-vm <id> [kernel rootfs]\n"))
				continue
			}
			logger.WithFields(logrus.Fields{"vm_id": id, "image": config.Image, "kernel": config.KernelPath, "rootfs": config.RootfsPath}).Info("Starting VM")
			err := backend.StartVM(context.Background(), config)
			if err != nil {
				logger.WithError(err).Error("Failed to start VM")
				conn.Write([]byte("error: " + err.Error() + "\n"))
			} else {
				runningVMs.Store(id, config)
				vmPublic, _, _ := ed25519.GenerateKey(rand.Reader)
				logger.WithFields(logrus.Fields{"vm_id": id, "public_key": fmt.Sprintf("%x", vmPublic)}).Info("Generated VM keypair")
				// Assume private key sent to VM somehow
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
				runningVMs.Delete(id)
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
				safeMode = true
				conn.Write([]byte("safe-mode enabled\n"))
			} else if action == "disable" {
				logger.Info("Disabling safe mode")
				safeMode = false
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
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		fmt.Println("Daemon not running")
		return
	}
	response := string(buf[:n])
	if jsonOutput {
		lines := strings.Split(strings.TrimSpace(response), "\n")
		status := map[string]interface{}{}
		for _, line := range lines {
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				key := parts[0]
				val := parts[1]
				switch key {
				case "Running VMs", "PID":
					if num, err := strconv.Atoi(val); err == nil {
						status[strings.ToLower(strings.ReplaceAll(key, " ", ""))] = num
					}
				case "Safe Mode":
					status["safeMode"] = val == "true"
				default:
					status[strings.ToLower(strings.ReplaceAll(key, " ", ""))] = val
				}
			}
		}
		jsonBytes, _ := json.Marshal(status)
		fmt.Println(string(jsonBytes))
	} else {
		fmt.Printf("Daemon status: %s\n", response)
	}
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
			if len(parts) == 2 {
				vms = append(vms, map[string]string{"id": parts[0], "image": parts[1]})
			}
		}
		jsonBytes, _ := json.Marshal(vms)
		fmt.Println(string(jsonBytes))
	} else {
		fmt.Printf("Running VMs:\n%s\n", response)
	}
}

func startVM(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte(fmt.Sprintf("start-vm %s", args[0])))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("VM start: %s", string(buf[:n]))
}

func stopVM(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte(fmt.Sprintf("stop-vm %s", args[0])))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("VM stop: %s", string(buf[:n]))
}

func statusVM(cmd *cobra.Command, args []string) {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Println("Daemon not running")
		return
	}
	defer conn.Close()

	conn.Write([]byte(fmt.Sprintf("status-vm %s", args[0])))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	fmt.Printf("VM status: %s", string(buf[:n]))
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
	startTime = time.Now()
	daemonPublicKey, daemonPrivateKey, _ = ed25519.GenerateKey(rand.Reader)
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
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "Run daemon in foreground")

	var statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		Run:   statusDaemon,
	}
	statusCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

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

	var vmCmd = &cobra.Command{
		Use:   "vm",
		Short: "Manage virtual machines",
	}

	var listCmd = &cobra.Command{
		Use:   "list",
		Short: "List running VMs",
		Run:   listVMs,
	}
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	var startVMCmd = &cobra.Command{
		Use:   "start <name>",
		Short: "Start a VM",
		Run:   startVM,
		Args:  cobra.ExactArgs(1),
	}

	var stopVMCmd = &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop a VM",
		Run:   stopVM,
		Args:  cobra.ExactArgs(1),
	}

	var statusVMCmd = &cobra.Command{
		Use:   "status <id>",
		Short: "Check VM status",
		Run:   statusVM,
		Args:  cobra.ExactArgs(1),
	}

	vmCmd.AddCommand(listCmd, startVMCmd, stopVMCmd, statusVMCmd)

	safeModeCmd.AddCommand(enableCmd, disableCmd)

	rootCmd.AddCommand(startCmd, statusCmd, doctorCmd, stopCmd, safeModeCmd, logsCmd, vmCmd)
	rootCmd.Execute()
}
