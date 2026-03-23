package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	vsockPort     = 1024
	workspaceDir  = "/workspace"
	secretsDir    = "/run/secrets"
	maxPayloadLen = 10 * 1024 * 1024 // 10 MB max payload
)

// Request is a JSON message from the kernel via vsock.
type Request struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Response is returned to the kernel.
type Response struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ExecPayload describes a command execution request.
type ExecPayload struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Dir     string   `json:"dir"`
	Timeout int      `json:"timeout_secs"`
}

// ExecResult holds the output of an executed command.
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// FileReadPayload describes a file read request.
type FileReadPayload struct {
	Path string `json:"path"`
}

// FileWritePayload describes a file write request.
type FileWritePayload struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    uint32 `json:"mode"`
}

// FileListPayload describes a directory listing request.
type FileListPayload struct {
	Path string `json:"path"`
}

// FileEntry is a single item in a directory listing.
type FileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"`
}

// StatusData reports the guest agent's current status.
type StatusData struct {
	Hostname  string `json:"hostname"`
	Uptime    string `json:"uptime"`
	Workspace bool   `json:"workspace_mounted"`
	PID       int    `json:"pid"`
}

// SecretInjectPayload is the set of secrets to write to tmpfs.
type SecretInjectPayload struct {
	Secrets []SecretItem `json:"secrets"`
}

// SecretItem is a single named secret to inject.
type SecretItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("guest-agent starting as PID 1")

	mountEssentialFS()

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("failed to create workspace directory: %v", err)
	}

	listener, err := listenVsock(vsockPort)
	if err != nil {
		log.Printf("vsock unavailable (%v), falling back to TCP", err)
		configureNetwork()
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", vsockPort))
		if err != nil {
			log.Fatalf("failed to listen on TCP port %d: %v", vsockPort, err)
		}
	}
	defer listener.Close()
	log.Printf("listening on %s", listener.Addr())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down", sig)
		cancel()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Println("guest-agent shutting down")
				return
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go handleConnection(ctx, conn)
	}
}

func handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(bufio.NewReaderSize(conn, 64*1024))
	encoder := json.NewEncoder(conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var req Request
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("decode error: %v", err)
			return
		}

		resp := dispatch(ctx, &req)
		if err := encoder.Encode(resp); err != nil {
			log.Printf("encode error: %v", err)
			return
		}
	}
}

func dispatch(ctx context.Context, req *Request) *Response {
	switch req.Type {
	case "exec":
		return handleExec(ctx, req)
	case "file.read":
		return handleFileRead(req)
	case "file.write":
		return handleFileWrite(req)
	case "file.list":
		return handleFileList(req)
	case "status":
		return handleStatus(req)
	case "secrets.inject":
		return handleSecretsInject(req)
	case "tool.invoke":
		return handleToolInvoke(req)
	default:
		return errorResponse(req.ID, fmt.Sprintf("unknown request type: %s", req.Type))
	}
}

func handleExec(ctx context.Context, req *Request) *Response {
	var payload ExecPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid exec payload: %v", err))
	}

	if payload.Command == "" {
		return errorResponse(req.ID, "command is required")
	}

	dir := payload.Dir
	if dir == "" {
		dir = workspaceDir
	}
	absDir, err := filepath.Abs(dir)
	if err != nil || !isUnderWorkspace(absDir) {
		return errorResponse(req.ID, "execution directory must be under /workspace")
	}

	timeout := time.Duration(payload.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, payload.Command, payload.Args...)
	cmd.Dir = absDir
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/workspace",
		"LANG=C.UTF-8",
	}

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("failed to start command: %v", err))
	}

	var stdout, stderr strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(&stdout, io.LimitReader(stdoutPipe, maxPayloadLen))
	}()
	go func() {
		defer wg.Done()
		io.Copy(&stderr, io.LimitReader(stderrPipe, maxPayloadLen))
	}()
	wg.Wait()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return errorResponse(req.ID, fmt.Sprintf("command failed: %v", err))
		}
	}

	result := ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	data, _ := json.Marshal(result)
	return &Response{
		ID:      req.ID,
		Success: exitCode == 0,
		Data:    data,
	}
}

func handleFileRead(req *Request) *Response {
	var payload FileReadPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid file.read payload: %v", err))
	}

	absPath, err := filepath.Abs(payload.Path)
	if err != nil || !isUnderWorkspace(absPath) {
		return errorResponse(req.ID, "path must be under /workspace")
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return errorResponse(req.ID, fmt.Sprintf("failed to read file: %v", err))
	}

	data, _ := json.Marshal(map[string]string{"content": string(content)})
	return &Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

func handleFileWrite(req *Request) *Response {
	var payload FileWritePayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid file.write payload: %v", err))
	}

	absPath, err := filepath.Abs(payload.Path)
	if err != nil || !isUnderWorkspace(absPath) {
		return errorResponse(req.ID, "path must be under /workspace")
	}

	mode := os.FileMode(payload.Mode)
	if mode == 0 {
		mode = 0644
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("failed to create directory: %v", err))
	}

	if err := os.WriteFile(absPath, []byte(payload.Content), mode); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("failed to write file: %v", err))
	}

	return &Response{
		ID:      req.ID,
		Success: true,
	}
}

func handleFileList(req *Request) *Response {
	var payload FileListPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid file.list payload: %v", err))
	}

	absPath, err := filepath.Abs(payload.Path)
	if err != nil || !isUnderWorkspace(absPath) {
		return errorResponse(req.ID, "path must be under /workspace")
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return errorResponse(req.ID, fmt.Sprintf("failed to list directory: %v", err))
	}

	files := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			Mode:  info.Mode().String(),
		})
	}

	data, _ := json.Marshal(files)
	return &Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

func handleStatus(req *Request) *Response {
	hostname, _ := os.Hostname()
	_, wsErr := os.Stat(workspaceDir)

	status := StatusData{
		Hostname:  hostname,
		Uptime:    readUptime(),
		Workspace: wsErr == nil,
		PID:       os.Getpid(),
	}

	data, _ := json.Marshal(status)
	return &Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

// isUnderWorkspace checks that a resolved path is strictly under /workspace.
func isUnderWorkspace(absPath string) bool {
	cleaned := filepath.Clean(absPath)
	return cleaned == workspaceDir || strings.HasPrefix(cleaned, workspaceDir+"/")
}

func handleSecretsInject(req *Request) *Response {
	var payload SecretInjectPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid secrets.inject payload: %v", err))
	}

	if len(payload.Secrets) == 0 {
		return errorResponse(req.ID, "no secrets provided")
	}

	// Ensure secretsDir is a tmpfs mount (created during mountEssentialFS via /run)
	if err := os.MkdirAll(secretsDir, 0700); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("failed to create secrets dir: %v", err))
	}

	injected := 0
	for _, s := range payload.Secrets {
		if s.Name == "" {
			return errorResponse(req.ID, "secret name must not be empty")
		}
		// Prevent path traversal
		if strings.Contains(s.Name, "/") || strings.Contains(s.Name, "..") {
			return errorResponse(req.ID, fmt.Sprintf("invalid secret name: %q", s.Name))
		}
		secretPath := filepath.Join(secretsDir, s.Name)
		// Write with owner-read-only permissions; never log the value
		if err := os.WriteFile(secretPath, []byte(s.Value), 0400); err != nil {
			return errorResponse(req.ID, fmt.Sprintf("failed to write secret %q: %v", s.Name, err))
		}
		injected++
	}

	log.Printf("injected %d secrets to %s", injected, secretsDir)
	data, _ := json.Marshal(map[string]int{"injected": injected})
	return &Response{
		ID:      req.ID,
		Success: true,
		Data:    data,
	}
}

// ToolInvokePayload is the request to invoke a skill tool.
type ToolInvokePayload struct {
	Tool string `json:"tool"`
	Args string `json:"args,omitempty"`
}

// ToolInvokeResult is the response from a tool invocation.
type ToolInvokeResult struct {
	Output string `json:"output"`
}

func handleToolInvoke(req *Request) *Response {
	var payload ToolInvokePayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid tool.invoke payload: %v", err))
	}
	if payload.Tool == "" {
		return errorResponse(req.ID, "tool name is required")
	}

	// Look for the tool as an executable under /workspace/tools/<name>.
	toolPath := filepath.Join(workspaceDir, "tools", payload.Tool)
	if _, err := os.Stat(toolPath); err == nil {
		// Execute the tool binary/script
		cmd := exec.Command(toolPath)
		if payload.Args != "" {
			cmd = exec.Command(toolPath, payload.Args)
		}
		cmd.Dir = workspaceDir
		cmd.Env = []string{
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"HOME=/workspace",
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errorResponse(req.ID, fmt.Sprintf("tool %q failed: %v (%s)", payload.Tool, err, string(out)))
		}
		result := ToolInvokeResult{Output: strings.TrimSpace(string(out))}
		data, _ := json.Marshal(result)
		return &Response{ID: req.ID, Success: true, Data: data}
	}

	// No deployed tool binary — return a placeholder so the demo flow is
	// visible.  The builder pipeline (step 6) will eventually deploy real
	// tool binaries into /workspace/tools/.
	log.Printf("tool.invoke: tool %q not found at %s, returning stub", payload.Tool, toolPath)
	result := ToolInvokeResult{
		Output: fmt.Sprintf("[stub] Tool %q invoked.  Deploy skill code to /workspace/tools/ via the builder pipeline.", payload.Tool),
	}
	data, _ := json.Marshal(result)
	return &Response{ID: req.ID, Success: true, Data: data}
}

func errorResponse(id, msg string) *Response {
	return &Response{
		ID:      id,
		Success: false,
		Error:   msg,
	}
}

func readUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	parts := strings.Fields(string(data))
	if len(parts) < 1 {
		return "unknown"
	}
	return parts[0] + "s"
}

func mountEssentialFS() {
	mounts := []struct {
		source string
		target string
		fstype string
		flags  uintptr
	}{
		{"proc", "/proc", "proc", 0},
		{"sysfs", "/sys", "sysfs", syscall.MS_RDONLY},
		{"devtmpfs", "/dev", "devtmpfs", 0},
		{"tmpfs", "/tmp", "tmpfs", 0},
		{"tmpfs", "/run", "tmpfs", 0},
	}

	for _, m := range mounts {
		os.MkdirAll(m.target, 0755)
		if err := syscall.Mount(m.source, m.target, m.fstype, m.flags, ""); err != nil {
			log.Printf("warning: failed to mount %s on %s: %v", m.fstype, m.target, err)
		}
	}
}

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

	file := os.NewFile(uintptr(fd), fmt.Sprintf("vsock:%d", port))
	listener, err := net.FileListener(file)
	file.Close()
	if err != nil {
		return nil, fmt.Errorf("FileListener: %w", err)
	}

	return listener, nil
}

// configureNetwork brings up eth0 using the kernel-assigned IP from DHCP or
// static boot params.  Firecracker assigns IPs via the host tap configuration.
func configureNetwork() {
	// Read the IP from kernel cmdline (set by host via boot_args).
	// Format: ... ip=<guest_ip>::<gateway>:<netmask>::eth0:off ...
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		log.Printf("warning: cannot read /proc/cmdline: %v", err)
		return
	}

	// Parse ip= parameter from kernel command line.
	var guestIP string
	for _, param := range strings.Fields(string(cmdline)) {
		if strings.HasPrefix(param, "ip=") {
			parts := strings.Split(param[3:], ":")
			if len(parts) > 0 {
				guestIP = parts[0]
			}
		}
	}

	if guestIP == "" {
		// Try bringing up eth0 with a simple link-local or DHCP.
		log.Println("no ip= kernel param, bringing up eth0 with DHCP")
		runNetCmd("/sbin/ifconfig", "eth0", "up")
		runNetCmd("/sbin/udhcpc", "-i", "eth0", "-n", "-q")
		return
	}

	log.Printf("configuring eth0 with IP %s", guestIP)
	runNetCmd("/sbin/ifconfig", "eth0", guestIP, "netmask", "255.255.255.252", "up")
}

func runNetCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("warning: %s %v failed: %v", name, args, err)
	}
}
