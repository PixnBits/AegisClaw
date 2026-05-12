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
var hubConn net.Conn
var hubEncoder *json.Encoder
var hubDecoder *json.Decoder
var hubMutex sync.Mutex
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

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
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
	logrus.Infof("Starting Firecracker VM %s with kernel %s, rootfs %s", config.ID, config.KernelPath, config.RootfsPath)
	configData := map[string]interface{}{
		"boot-source": map[string]interface{}{
			"kernel_image_path": config.KernelPath,
			"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off nomodules",
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
	err := os.WriteFile(configPath, configBytes, 0666)
	if err != nil {
		logrus.Errorf("Failed to write config file %s: %v", configPath, err)
		return err
	}
	logrus.Infof("Config written to %s", configPath)
	cmd := exec.CommandContext(ctx, "firecracker", "--api-sock", sockPath, "--config-file", configPath)
	err = cmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Firecracker: %v", err)
		return err
	}
	logrus.Infof("Firecracker process started, PID %d", cmd.Process.Pid)
	runningCmds.Store(config.ID, cmd)
	// Wait for firecracker to be ready
	time.Sleep(10 * time.Second)
	logrus.Infof("Checking if Firecracker process is alive")
	if cmd.Process == nil || cmd.Process.Signal(syscall.Signal(0)) != nil {
		logrus.Error("Firecracker process died")
		runningCmds.Delete(config.ID)
		return fmt.Errorf("Firecracker process died")
	}
	logrus.Infof("Checking if socket %s is created", sockPath)
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		logrus.Errorf("Firecracker socket not created: %v", err)
		cmd.Process.Kill()
		runningCmds.Delete(config.ID)
		return err
	}
	logrus.Info("Socket created, VM launched successfully")
	logrus.Infof("VM %s started successfully", config.ID)
	return nil
}

func (f *FirecrackerBackend) StopVM(ctx context.Context, id string) error {
	sockPath := "/tmp/firecracker-" + id + ".sock"
	logrus.Infof("Stopping Firecracker VM %s", id)
	// Send halt action
	sendAPIRequest(sockPath, "PUT", "/actions", map[string]string{"action_type": "InstanceHalt"})
	if cmd, ok := runningCmds.Load(id); ok {
		c := cmd.(*exec.Cmd)
		c.Process.Kill()
		runningCmds.Delete(id)
		runningVMs.Delete(id)
	}
	logrus.Infof("VM %s stopped", id)
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
var headless bool

func isDaemonRunning() bool {
	// Check PID file first
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
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return false
	}

	// Also check socket for additional verification
	socket := expandPath(socketPath)
	conn, err := net.DialTimeout("unix", socket, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
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

	// Start AegisHub
	hubSocket := expandPath("~/.aegis/hub.sock")
	hubCmd := exec.Command("./bin/aegishub", "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	err = hubCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start AegisHub: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("hub", hubCmd)

	// Wait for hub
	time.Sleep(2 * time.Second)

	// Start Memory VM
	memCmd := exec.Command("./bin/memory")
	memCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	memCmd.Stdout = os.Stdout
	memCmd.Stderr = os.Stderr
	err = memCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Memory VM: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("memory", memCmd)

	// Start Store VM
	storeCmd := exec.Command("./bin/store")
	storeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	err = storeCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Store VM: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("store", storeCmd)

	// Start Web Portal
	webCmd := exec.Command("./bin/web-portal")
	webCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	err = webCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Web Portal: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("web-portal", webCmd)

	// Start Agent Runtime
	agentCmd := exec.Command("./bin/agent")
	agentCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	agentCmd.Stdout = os.Stdout
	agentCmd.Stderr = os.Stderr
	err = agentCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Agent Runtime: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("agent", agentCmd)

	// Start Builder VM
	builderCmd := exec.Command("./bin/builder")
	builderCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	builderCmd.Stdout = os.Stdout
	builderCmd.Stderr = os.Stderr
	err = builderCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Builder VM: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("builder", builderCmd)

	// Start Network Boundary
	netCmd := exec.Command("./bin/network-boundary")
	netCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	err = netCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Network Boundary: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("network-boundary", netCmd)

	// Start Court Scribe
	scribeCmd := exec.Command("./bin/court-scribe")
	scribeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	err = scribeCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Court Scribe: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("court-scribe", scribeCmd)

	// Start Court Personas (for simplicity, start one)
	personaCmd := exec.Command("./bin/court-persona", "--persona", "ciso")
	personaCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+hubSocket)
	err = personaCmd.Start()
	if err != nil {
		logrus.Errorf("Failed to start Court Persona: %v", err)
		os.Exit(1)
	}
	runningCmds.Store("court-persona", personaCmd)

	// Connect to hub (main connection for command-response)
	hubConn, err = net.Dial("unix", hubSocket)
	if err != nil {
		logrus.Errorf("Failed to connect to hub: %v", err)
		os.Exit(1)
	}

	// Initialize persistent encoder/decoder for main connection
	hubEncoder = json.NewEncoder(hubConn)
	hubDecoder = json.NewDecoder(hubConn)

	// Register with hub
	regMsg := Message{
		Source:      "daemon",
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	err = hubEncoder.Encode(regMsg)
	if err != nil {
		logrus.Errorf("Failed to register with hub: %v", err)
		os.Exit(1)
	}
	var resp map[string]interface{}
	err = hubDecoder.Decode(&resp)
	if err != nil {
		logrus.Errorf("Failed to decode hub response: %v", err)
		os.Exit(1)
	}
	logrus.Info("Registered with AegisHub")

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
	// Handle CLI connections
	for {
		select {
		case <-done:
			fmt.Println("Daemon stopping...")
			stopAllVMs()
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go handleCLIConnection(conn, done)
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
			hubStatus := "stopped"
			if cmd, ok := runningCmds.Load("hub"); ok {
				c := cmd.(*exec.Cmd)
				if c.Process != nil && c.Process.Signal(syscall.Signal(0)) == nil {
					hubStatus = "running"
				}
			}
			memoryStatus := "stopped"
			if cmd, ok := runningCmds.Load("memory"); ok {
				c := cmd.(*exec.Cmd)
				if c.Process != nil && c.Process.Signal(syscall.Signal(0)) == nil {
					memoryStatus = "running"
				}
			}
			storeStatus := "stopped"
			if cmd, ok := runningCmds.Load("store"); ok {
				c := cmd.(*exec.Cmd)
				if c.Process != nil && c.Process.Signal(syscall.Signal(0)) == nil {
					storeStatus = "running"
				}
			}
			response := fmt.Sprintf("Daemon: running\nBackend: %s\nSafe Mode: %t\nRunning VMs: %d\nUptime: %v\nPID: %d\nHub: %s\nMemory VM: %s\nStore VM: %s\n", backendName, safeMode, count, time.Since(startTime).Round(time.Second), os.Getpid(), hubStatus, memoryStatus, storeStatus)
			conn.Write([]byte(response))
		case "memory.get_context":
			logger.Info("Memory get context requested")
			msg := Message{
				Source:      "daemon",
				Destination: "memory",
				Command:     "memory.get_context",
				Payload:     nil,
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "dummy",
			}
			hubMutex.Lock()
			err := hubEncoder.Encode(msg)
			if err != nil {
				hubMutex.Unlock()
				conn.Write([]byte("error: failed to send to memory\n"))
				continue
			}
			var resp Message
			err = hubDecoder.Decode(&resp)
			hubMutex.Unlock()
			if err != nil {
				conn.Write([]byte("error: failed to receive from memory\n"))
				continue
			}
			response := fmt.Sprintf("Memory context: %v\n", resp.Payload)
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
			// Stop hub, memory, store
			for _, name := range []string{"hub", "memory", "store"} {
				if cmd, ok := runningCmds.Load(name); ok {
					c := cmd.(*exec.Cmd)
					c.Process.Kill()
					runningCmds.Delete(name)
				}
			}
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
	data, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Printf("Error reading logs: %v\n", err)
		return
	}
	fmt.Print(string(data))
}

func listSkills(cmd *cobra.Command, args []string) {
	resp := sendToHub("store", "skill.list", nil)
	if jsonOutput {
		fmt.Println(resp)
	} else {
		fmt.Println("Skills: (JSON output for parsing)")
	}
}

func proposeSkill(cmd *cobra.Command, args []string) {
	fmt.Println("Propose a new skill via chat or provide description.")
}

func listCourtDecisions(cmd *cobra.Command, args []string) {
	resp := sendToHub("store", "court.get_reviews", map[string]interface{}{"id": "all"})
	if jsonOutput {
		fmt.Println(resp)
	} else {
		fmt.Println("Court decisions: (JSON output for parsing)")
	}
}

func startChat(cmd *cobra.Command, args []string) {
	fmt.Println("Starting chat session...")
	if headless {
		fmt.Println("Running in headless mode.")
	}
}

func listSessions(cmd *cobra.Command, args []string) {
	fmt.Println("Sessions: (mock list)")
}

func listTasks(cmd *cobra.Command, args []string) {
	fmt.Println("Tasks: (mock list)")
}

func showAutonomy(cmd *cobra.Command, args []string) {
	sessionID := args[0]
	fmt.Printf("Autonomy for session %s: (mock data)\n", sessionID)
}

func showAuditLog(cmd *cobra.Command, args []string) {
	resp := sendToHub("store", "audit.get_root", nil)
	if jsonOutput {
		fmt.Println(resp)
	} else {
		fmt.Println("Audit root:", resp)
	}
}

func handleCLIConnection(conn net.Conn, done chan bool) {
	defer conn.Close()
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return
	}
	cmd := string(buf[:n])
	cmd = strings.TrimSpace(cmd)

	switch cmd {
	case "status":
		response := getDaemonStatus()
		conn.Write([]byte(response))
	case "vm list":
		response := getVMList()
		conn.Write([]byte(response))
	case "stop":
		conn.Write([]byte("stopping"))
		done <- true
	default:
		conn.Write([]byte("Unknown command"))
	}
}

func stopAllVMs() {
	runningCmds.Range(func(key, value interface{}) bool {
		cmd := value.(*exec.Cmd)
		cmd.Process.Kill()
		return true
	})
}

func getDaemonStatus() string {
	hubStatus, memoryStatus, storeStatus := "unknown", "unknown", "unknown"

	// Check hub
	if hubConn != nil {
		hubStatus = "running"
		// Assume VMs are running if hub is
		memoryStatus = "running"
		storeStatus = "running"
	}

	count := 0
	runningCmds.Range(func(key, value interface{}) bool {
		name := key.(string)
		if name != "hub" { // Don't count hub as VM
			count++
		}
		return true
	})

	uptime := time.Since(startTime).Round(time.Second)
	backend := "Firecracker"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		backend = "Docker"
	}
	response := fmt.Sprintf("Daemon: running\nBackend: %s\nSafe Mode: %t\nRunning VMs: %d\nUptime: %v\nPID: %d\nHub: %s\nMemory VM: %s\nStore VM: %s\n", backend, safeMode, count, uptime, os.Getpid(), hubStatus, memoryStatus, storeStatus)
	return response
}

func getVMVersion(vmName string) string {
	// Debug logging
	debugFile := expandPath("~/.aegis/getversion.log")
	f, _ := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "[%s] Querying version for %s\n", time.Now().Format("15:04:05"), vmName)
	}

	// Query hub using a fresh connection (won't overwrite persistent registration)
	socket := expandPath("~/.aegis/hub.sock")
	conn, err := net.Dial("unix", socket)
	if err != nil {
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Failed to connect to hub for %s: %v\n", time.Now().Format("15:04:05"), vmName, err)
			f.Close()
		}
		return "unknown"
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Channel to receive responses from reader goroutine (as generic JSON)
	responseChan := make(chan json.RawMessage, 10)  // Larger buffer to hold multiple responses
	errChan := make(chan error, 1)
	
	// Start a goroutine to read responses from the hub
	go func() {
		for {
			var rawMsg json.RawMessage
			if err := decoder.Decode(&rawMsg); err != nil {
				errChan <- err
				return
			}
			// Send response through channel (blocking send if buffer full)
			responseChan <- rawMsg
		}
	}()

	// Register with the hub (will get a temporary ID like daemon-temp-1)
	regMsg := Message{
		Source:      "daemon",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]interface{}{
			"public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", // dummy ed25519 public key (32 bytes base64)
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Signature: "dummy",
	}

	if err := encoder.Encode(regMsg); err != nil {
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Failed to send register to hub for %s: %v\n", time.Now().Format("15:04:05"), vmName, err)
			f.Close()
		}
		return "unknown"
	}

	// Read registration response
	var regResp map[string]interface{}
	select {
	case rawMsg := <-responseChan:
		if err := json.Unmarshal(rawMsg, &regResp); err != nil {
			if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
				fmt.Fprintf(f, "[%s] Failed to parse registration response for %s: %v\n", time.Now().Format("15:04:05"), vmName, err)
				f.Close()
			}
			return "unknown"
		}
	case err := <-errChan:
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Error reading registration response for %s: %v\n", time.Now().Format("15:04:05"), vmName, err)
			f.Close()
		}
		return "unknown"
	case <-time.After(2 * time.Second):
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Timeout reading registration response for %s\n", time.Now().Format("15:04:05"), vmName)
			f.Close()
		}
		return "unknown"
	}
	
	if errMsg, ok := regResp["error"].(string); ok {
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Registration error for %s: %s\n", time.Now().Format("15:04:05"), vmName, errMsg)
			f.Close()
		}
		return "unknown"
	}
	
	// Get the assigned ID from registration response
	assignedID := "daemon"  // default
	if assignedIDVal, ok := regResp["assigned_id"].(string); ok {
		assignedID = assignedIDVal
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Fresh connection registered with ID: %s\n", time.Now().Format("15:04:05"), assignedID)
			f.Close()
		}
	}

	// Send get-version query using the assigned ID
	query := Message{
		Source:      assignedID,  // Use the assigned ID so responses come back to this connection
		Destination: vmName,
		Command:     "get-version",
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}

	if err := encoder.Encode(query); err != nil {
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Failed to send query to hub for %s: %v\n", time.Now().Format("15:04:05"), vmName, err)
			f.Close()
		}
		return "unknown"
	}

	// Read response from fresh connection (via goroutine channel)
	var response Message
	select {
	case rawMsg := <-responseChan:
		if err := json.Unmarshal(rawMsg, &response); err != nil {
			if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
				fmt.Fprintf(f, "[%s] Failed to parse response for %s: %v (rawMsg=%s)\n", time.Now().Format("15:04:05"), vmName, err, string(rawMsg))
				f.Close()
			}
			return "unknown"
		}
	case err := <-errChan:
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Error reading response for %s: %v\n", time.Now().Format("15:04:05"), vmName, err)
			f.Close()
		}
		return "unknown"
	case <-time.After(3 * time.Second):
		if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
			fmt.Fprintf(f, "[%s] Timeout reading response for %s\n", time.Now().Format("15:04:05"), vmName)
			f.Close()
		}
		return "unknown"
	}

	// Extract version from response payload
	if payload, ok := response.Payload.(map[string]interface{}); ok {
		if version, ok := payload["version"].(string); ok {
			if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
				fmt.Fprintf(f, "[%s] Got version for %s: %s\n", time.Now().Format("15:04:05"), vmName, version)
				f.Close()
			}
			return version
		}
	}
	if f, err2 := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err2 == nil {
		fmt.Fprintf(f, "[%s] Version not found in response for %s: source=%s dest=%s cmd=%s payload=%v\n", time.Now().Format("15:04:05"), vmName, response.Source, response.Destination, response.Command, response.Payload)
		f.Close()
	}

	return "unknown"
}

func debugLog(msg string) {
	f, _ := os.OpenFile("/tmp/aegis-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if f != nil {
		defer f.Close()
		fmt.Fprintln(f, time.Now().Format("15:04:05.000"), msg)
	}
}

func getVMList() string {
	vms := []map[string]string{}
	runningCmds.Range(func(key, value interface{}) bool {
		name := key.(string)
		if name != "hub" { // hub is not a VM
			version := getVMVersion(name)
			vms = append(vms, map[string]string{"name": name, "version": version})
		}
		return true
	})

	if len(vms) == 0 {
		return "Running VMs:\nNo running VMs"
	}

	response := "Running VMs:\n"
	for _, vm := range vms {
		response += fmt.Sprintf("- %s (version: %s)\n", vm["name"], vm["version"])
	}
	return response
}

func sendToHubInternal(destination, command string) string {
	if hubConn == nil {
		return ""
	}
	msg := Message{
		Source:      "daemon",
		Destination: destination,
		Command:     command,
		Payload:     nil,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	hubMutex.Lock()
	defer hubMutex.Unlock()

	err := hubEncoder.Encode(msg)
	if err != nil {
		return ""
	}
	var resp Message
	err = hubDecoder.Decode(&resp)
	if err != nil {
		return ""
	}
	return resp.Command
}

func sendToHub(destination, command string, payload interface{}) string {
	socket := expandPath(socketPath)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return fmt.Sprintf("Error connecting: %v", err)
	}
	defer conn.Close()

	msg := Message{
		Source:      "cli",
		Destination: destination,
		Command:     command,
		Payload:     payload,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	err = encoder.Encode(msg)
	if err != nil {
		return fmt.Sprintf("Error sending: %v", err)
	}

	var resp Message
	err = decoder.Decode(&resp)
	if err != nil {
		return fmt.Sprintf("Error receiving: %v", err)
	}

	data, _ := json.Marshal(resp)
	return string(data)
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

	// Skills commands
	var skillsCmd = &cobra.Command{
		Use:   "skills",
		Short: "Manage skills",
	}
	var skillsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List skills",
		Run:   listSkills,
	}
	skillsListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	var skillsProposeCmd = &cobra.Command{
		Use:   "propose",
		Short: "Propose a new skill",
		Run:   proposeSkill,
	}
	skillsCmd.AddCommand(skillsListCmd, skillsProposeCmd)

	// Court commands
	var courtCmd = &cobra.Command{
		Use:   "court",
		Short: "Manage court decisions",
	}
	var courtDecisionsCmd = &cobra.Command{
		Use:   "decisions",
		Short: "Manage court decisions",
	}
	var courtDecisionsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List court decisions",
		Run:   listCourtDecisions,
	}
	courtDecisionsListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	courtDecisionsCmd.AddCommand(courtDecisionsListCmd)
	courtCmd.AddCommand(courtDecisionsCmd)

	// Chat command
	var chatCmd = &cobra.Command{
		Use:   "chat",
		Short: "Start chat session",
		Run:   startChat,
	}
	chatCmd.Flags().BoolVar(&headless, "headless", false, "Run in headless mode")

	// Sessions commands
	var sessionsCmd = &cobra.Command{
		Use:   "sessions",
		Short: "Manage chat sessions",
	}
	var sessionsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List sessions",
		Run:   listSessions,
	}
	sessionsListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	sessionsCmd.AddCommand(sessionsListCmd)

	// Tasks commands
	var tasksCmd = &cobra.Command{
		Use:   "tasks",
		Short: "Manage tasks",
	}
	var tasksListCmd = &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		Run:   listTasks,
	}
	tasksListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	tasksCmd.AddCommand(tasksListCmd)

	// Autonomy commands
	var autonomyCmd = &cobra.Command{
		Use:   "autonomy",
		Short: "Manage autonomy settings",
	}
	var autonomyShowCmd = &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show autonomy for session",
		Run:   showAutonomy,
		Args:  cobra.ExactArgs(1),
	}
	autonomyCmd.AddCommand(autonomyShowCmd)

	// Audit commands
	var auditCmd = &cobra.Command{
		Use:   "audit",
		Short: "Audit and verification",
	}
	var auditLogCmd = &cobra.Command{
		Use:   "log",
		Short: "Show audit log",
		Run:   showAuditLog,
	}
	auditLogCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	auditCmd.AddCommand(auditLogCmd)

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

	rootCmd.AddCommand(startCmd, statusCmd, doctorCmd, stopCmd, safeModeCmd, logsCmd, vmCmd, skillsCmd, courtCmd, chatCmd, sessionsCmd, tasksCmd, autonomyCmd, auditCmd)
	rootCmd.Execute()
}
