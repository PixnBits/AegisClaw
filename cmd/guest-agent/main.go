package main

import (
	"bufio"
	"bytes"
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

	// unixSocketPath is the AF_UNIX socket path used when --transport=unix.
	// It lives inside the bind-mounted /run/aegis directory so the host can
	// reach it via the state directory without entering the container network
	// namespace.
	unixSocketPath = "/run/aegis/agent.sock"
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

	// --transport selects the IPC mechanism:
	//   vsock (default) — AF_VSOCK port 1024 with TCP fallback (Firecracker mode)
	//   unix            — AF_UNIX /run/aegis/agent.sock (Docker mode)
	//
	// Accepted forms: --transport=unix  or  --transport unix
	transport := parseTransportFlag(os.Args[1:])

	mountEssentialFS()

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		log.Fatalf("failed to create workspace directory: %v", err)
	}

	var listener net.Listener
	var err error

	switch transport {
	case "unix":
		// Docker mode: listen on a Unix domain socket inside the bind-mounted
		// /run/aegis directory.  The parent directory must already exist (it is
		// bind-mounted from the host state directory).
		if err = os.MkdirAll("/run/aegis", 0755); err != nil {
			log.Fatalf("failed to create /run/aegis: %v", err)
		}
		// Remove a stale socket from a previous container invocation.
		_ = os.Remove(unixSocketPath)
		listener, err = net.Listen("unix", unixSocketPath)
		if err != nil {
			log.Fatalf("failed to listen on unix socket %s: %v", unixSocketPath, err)
		}
		log.Printf("listening on unix:%s", unixSocketPath)
	default:
		// vsock mode (Firecracker): the virtio_vsock device probe runs
		// asynchronously so retry for up to 2 seconds before falling back to TCP.
		for attempt := 0; attempt < 20; attempt++ {
			listener, err = listenVsock(vsockPort)
			if err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if listener == nil {
			log.Printf("vsock unavailable after retries (%v), falling back to TCP", err)
			configureNetwork()
			listener, err = net.Listen("tcp", fmt.Sprintf(":%d", vsockPort))
			if err != nil {
				log.Fatalf("failed to listen on TCP port %d: %v", vsockPort, err)
			}
		}
		log.Printf("listening on %s", listener.Addr())
	}
	defer listener.Close()

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
	case "secrets.inject", "secrets.refresh":
		// Both inject and refresh use the same handler: write/overwrite files
		// in /run/secrets/<name> on tmpfs.  refresh is the idempotent variant
		// used after a rotation so running skill code picks up the new value
		// on its next /run/secrets/<name> read without a full VM restart.
		return handleSecretsInject(req)
	case "tool.invoke":
		return handleToolInvoke(req)
	case "review.execute":
		return handleReviewExecute(ctx, req)
	case "chat.message":
		return handleChatMessage(ctx, req)
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

type executeScriptPayload struct {
	Language  string   `json:"language"`
	Code      string   `json:"code"`
	Args      []string `json:"args"`
	TimeoutMS int      `json:"timeout_ms"`
}

func handleToolInvoke(req *Request) *Response {
	var payload ToolInvokePayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid tool.invoke payload: %v", err))
	}
	if payload.Tool == "" {
		return errorResponse(req.ID, "tool name is required")
	}

	// Prevent path traversal: tool names must not contain separators or ".."
	// even though we are already inside a Firecracker-isolated rootfs.
	if strings.ContainsAny(payload.Tool, "/\\") || strings.Contains(payload.Tool, "..") {
		return errorResponse(req.ID, fmt.Sprintf("invalid tool name: %q", payload.Tool))
	}

	if payload.Tool == "execute_script" {
		return handleExecuteScript(req.ID, payload.Args)
	}

	// Look for the tool as an executable under /workspace/tools/<name>.
	toolPath := filepath.Join(workspaceDir, "tools", payload.Tool)
	// Guard against any residual traversal after Clean (defence-in-depth).
	if !isUnderWorkspace(toolPath) {
		return errorResponse(req.ID, fmt.Sprintf("tool path escapes workspace: %q", payload.Tool))
	}
	if _, err := os.Stat(toolPath); err == nil {
		// Execute the tool binary/script; cap combined output to maxPayloadLen.
		var outBuf, errBuf bytes.Buffer
		var cmd *exec.Cmd
		if payload.Args != "" {
			cmd = exec.Command(toolPath, payload.Args)
		} else {
			cmd = exec.Command(toolPath)
		}
		cmd.Dir = workspaceDir
		cmd.Env = []string{
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"HOME=/workspace",
		}
		cmd.Stdout = &limitWriter{w: &outBuf, limit: maxPayloadLen}
		cmd.Stderr = &limitWriter{w: &errBuf, limit: maxPayloadLen}
		if err := cmd.Run(); err != nil {
			combined := strings.TrimSpace(outBuf.String())
			if e := strings.TrimSpace(errBuf.String()); e != "" {
				combined += "\n" + e
			}
			return errorResponse(req.ID, fmt.Sprintf("tool %q failed: %v (%s)", payload.Tool, err, combined))
		}
		result := ToolInvokeResult{Output: strings.TrimSpace(outBuf.String())}
		data, _ := json.Marshal(result)
		return &Response{ID: req.ID, Success: true, Data: data}
	}

	// No deployed tool binary — return a not-found error so callers can
	// distinguish a missing tool from a successful empty result.
	return errorResponse(req.ID, fmt.Sprintf("tool %q not found; deploy via the builder pipeline to /workspace/tools/", payload.Tool))
}

// limitWriter is an io.Writer that caps total bytes written at limit.
type limitWriter struct {
	w     *bytes.Buffer
	limit int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	remaining := lw.limit - lw.w.Len()
	if remaining <= 0 {
		log.Printf("limitWriter: output truncated at %d bytes; discarding %d further bytes", lw.limit, len(p))
		return len(p), nil // discard excess; caller sees full write length to avoid short-write errors
	}
	if len(p) > remaining {
		log.Printf("limitWriter: output approaching limit (%d bytes); truncating write of %d bytes", lw.limit, len(p))
		p = p[:remaining]
	}
	return lw.w.Write(p)
}

func handleExecuteScript(requestID, rawArgs string) *Response {
	var payload executeScriptPayload
	if err := json.Unmarshal([]byte(rawArgs), &payload); err != nil {
		return errorResponse(requestID, fmt.Sprintf("invalid execute_script args: %v", err))
	}

	payload.Language = strings.ToLower(strings.TrimSpace(payload.Language))
	if payload.Language == "" {
		return errorResponse(requestID, "execute_script requires language")
	}
	if strings.TrimSpace(payload.Code) == "" {
		return errorResponse(requestID, "execute_script requires non-empty code")
	}

	timeout := time.Duration(payload.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}

	runtimeByLanguage := map[string][]string{
		"python":     {"python3", "-c"},
		"javascript": {"node", "-e"},
		"bash":       {"bash", "-c"},
		"sh":         {"sh", "-c"},
	}
	runtimeCmd, ok := runtimeByLanguage[payload.Language]
	if !ok {
		return errorResponse(requestID, fmt.Sprintf("unsupported script language %q", payload.Language))
	}

	cmdCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := append([]string{}, runtimeCmd[1:]...)
	cmdArgs = append(cmdArgs, payload.Code)
	cmdArgs = append(cmdArgs, payload.Args...)
	cmd := exec.CommandContext(cmdCtx, runtimeCmd[0], cmdArgs...)
	cmd.Dir = workspaceDir
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/workspace",
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	const maxOutputLen = 64 * 1024
	truncate := func(s string) string {
		if len(s) <= maxOutputLen {
			return s
		}
		return s[:maxOutputLen] + "\n[output truncated]"
	}
	outText := strings.TrimSpace(truncate(stdout.String()))
	errText := strings.TrimSpace(truncate(stderr.String()))

	if cmdCtx.Err() == context.DeadlineExceeded {
		return errorResponse(requestID, fmt.Sprintf("script timed out after %s", timeout))
	}
	if err != nil {
		msg := fmt.Sprintf("script failed: %v", err)
		if errText != "" {
			msg += ": " + errText
		}
		return errorResponse(requestID, msg)
	}

	if outText == "" && errText != "" {
		outText = errText
	}
	if outText == "" {
		outText = "(no output)"
	}

	data, _ := json.Marshal(ToolInvokeResult{Output: outText})
	return &Response{ID: requestID, Success: true, Data: data}
}

func errorResponse(id, msg string) *Response {
	return &Response{
		ID:      id,
		Success: false,
		Error:   msg,
	}
}

// parseTransportFlag parses the --transport flag from args, returning the
// transport name.  The flag can appear as --transport=<value> or as two
// separate tokens --transport <value>.  Defaults to "vsock".
func parseTransportFlag(args []string) string {
	for i, arg := range args {
		if strings.HasPrefix(arg, "--transport=") {
			return strings.TrimPrefix(arg, "--transport=")
		}
		if arg == "--transport" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return "vsock"
}

// ReviewExecutePayload is received from the kernel control plane (D1).
// It mirrors internal/court.ReviewRequest.
type ReviewExecutePayload struct {
	ProposalID  string          `json:"proposal_id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Spec        json.RawMessage `json:"spec,omitempty"`
	PersonaName string          `json:"persona_name"`
	PersonaRole string          `json:"persona_role"`
	Prompt      string          `json:"prompt"`
	Model       string          `json:"model"`
	Round       int             `json:"round"`
	Temperature *float64        `json:"temperature,omitempty"`
	Seed        int64           `json:"seed,omitempty"`
}

// handleReviewExecute runs a Court review inside this sandbox (D1).
//
// The guest agent receives the review prompt, calls the Ollama endpoint
// (which the sandbox networking allows on 127.0.0.1:11434), and returns
// the structured JSON verdict. This ensures that no review request
// reaches Ollama from the host daemon process.
func handleReviewExecute(ctx context.Context, req *Request) *Response {
	var payload ReviewExecutePayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid review.execute payload: %v", err))
	}

	if payload.Model == "" {
		return errorResponse(req.ID, "model is required")
	}
	if payload.Prompt == "" {
		return errorResponse(req.ID, "prompt is required")
	}

	// Build the review user message.
	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "Review the following proposal (round %d):\n\n", payload.Round)
	fmt.Fprintf(&userMsg, "Proposal ID: %s\n", payload.ProposalID)
	fmt.Fprintf(&userMsg, "Title: %s\n", payload.Title)
	fmt.Fprintf(&userMsg, "Description: %s\n", payload.Description)
	fmt.Fprintf(&userMsg, "Category: %s\n", payload.Category)
	if len(payload.Spec) > 0 {
		fmt.Fprintf(&userMsg, "Spec: %s\n", string(payload.Spec))
	}
	userMsg.WriteString("\nRespond with a JSON object containing:\n")
	userMsg.WriteString(`- "verdict": one of "approve", "reject", "ask", "abstain"` + "\n")
	userMsg.WriteString(`- "risk_score": a number between 0 and 10` + "\n")
	userMsg.WriteString(`- "evidence": an array of strings supporting your verdict` + "\n")
	userMsg.WriteString(`- "questions": (optional) an array of follow-up questions` + "\n")
	userMsg.WriteString(`- "comments": a brief summary of your assessment` + "\n")

	// Call Ollama via the host LLM proxy over vsock (no network interface in
	// this sandbox — all LLM access goes through the kernel vsock channel).
	messages := []map[string]string{
		{"role": "system", "content": payload.Prompt},
		{"role": "user", "content": userMsg.String()},
	}
	defaultTemperature := 0.3
	options := buildOllamaOptions(payload.Temperature, payload.Seed, &defaultTemperature)

	proxyCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	raw, _, _, err := callOllamaViaProxy(proxyCtx, payload.Model, "", messages, "json", options)
	if err != nil {
		return errorResponse(req.ID, fmt.Sprintf("ollama proxy error: %v", err))
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errorResponse(req.ID, "empty response from model")
	}

	// Validate the JSON structure before returning.
	var check map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &check); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("model returned invalid JSON: %v", err))
	}

	log.Printf("review.execute: model=%s persona=%s verdict=%v", payload.Model, payload.PersonaName, check["verdict"])

	data := json.RawMessage(raw)
	return &Response{ID: req.ID, Success: true, Data: data}
}

// ChatMessagePayload is received from the daemon for D2 (main-agent sandbox).
type ChatMessagePayload struct {
	Messages    []ChatMsg `json:"messages"`
	Model       string    `json:"model"`
	StreamID    string    `json:"stream_id,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	Seed        int64     `json:"seed,omitempty"`
	// StructuredOutput, when true, instructs the agent VM to enforce JSON-format
	// responses from Ollama and validate tool-call JSON before returning.
	// This is the Phase 0 structured output enforcement mechanism.
	StructuredOutput bool `json:"structured_output,omitempty"`
}

// ChatMsg represents a single message in the conversation.
// Name is used for tool-result messages (role=="tool").
type ChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ChatResponse is the response from handleChatMessage.
// Status is either "final" (done) or "tool_call" (agent wants a tool executed).
type ChatResponse struct {
	Status   string `json:"status"`            // "final" | "tool_call"
	Role     string `json:"role,omitempty"`    // present when status=="final"
	Content  string `json:"content,omitempty"` // present when status=="final"
	Thinking string `json:"thinking,omitempty"`
	Tool     string `json:"tool,omitempty"` // present when status=="tool_call"
	Args     string `json:"args,omitempty"` // present when status=="tool_call"
}

const (
	reactMaxToolCalls = 10
	// ollamaTimeout gives thinking models (e.g. qwen3, deepseek-r1) enough
	// time to reason before producing their first output token.
	ollamaTimeout = 300 * time.Second

	// structuredOutputCorrectionPrompt is sent to the model as a second-chance
	// prompt when it fails to return valid JSON in structured-output mode.
	structuredOutputCorrectionPrompt = `Your previous response was not valid JSON. ` +
		`Respond ONLY with a JSON object: ` +
		`{"status":"final","content":"<your answer>"} OR {"status":"tool_call","tool":"<name>","args":{...}}`
)

// toolCallMarkers lists fence markers in priority order. Plain "```" is last
// because it also matches the prefix of the tagged variants.
var toolCallMarkers = []string{"```tool-call", "```json", "```"}

// parseAgentToolCall extracts the first tool-call block from raw LLM content.
// Supports both modern {"name":"...","args":{}} and legacy {"skill":"...","tool":"..."} formats.
// Returns ("", "", false) when no valid block is found.
//
// Handles three cases:
//  1. Fenced with closing fence: ```tool-call\n{...}\n```
//  2. Fenced without closing fence: ```tool-call\n{...}  (small models often omit the closing fence)
//  3. Bare JSON: {"name": "...", "args": {...}}  (no fences at all)
func parseAgentToolCall(content string) (toolName, argsJSON string, found bool) {
	// Phase 1: Try each fence marker, first with closing fence, then without.
	for _, marker := range toolCallMarkers {
		search := content
		for {
			start := strings.Index(search, marker)
			if start < 0 {
				break
			}
			after := search[start+len(marker):]
			end := strings.Index(after, "```")

			var block string
			if end >= 0 {
				block = strings.TrimSpace(after[:end])
			} else {
				// No closing fence — try using everything after the marker.
				block = strings.TrimSpace(after)
			}

			if name, args, ok := tryParseToolJSON(block); ok {
				return name, args, true
			}

			if end < 0 {
				break // no closing fence, no point continuing with this marker
			}
			search = after[end+3:]
		}
	}

	// Phase 2: Bare JSON fallback — look for {"name": anywhere in the content.
	if idx := strings.Index(content, `{"name"`); idx >= 0 {
		candidate := content[idx:]
		if name, args, ok := tryParseToolJSON(candidate); ok {
			return name, args, true
		}
	}

	return "", "", false
}

// tryParseToolJSON attempts to parse a string as a tool-call JSON object.
// Tries modern format first, then legacy format.
func tryParseToolJSON(block string) (toolName, argsJSON string, found bool) {
	// Modern format: {"name": "tool_name", "args": {...}}
	var modern struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(block), &modern); err == nil && modern.Name != "" {
		argsStr := "{}"
		if len(modern.Args) > 0 {
			argsStr = string(modern.Args)
		}
		return modern.Name, argsStr, true
	}

	// Legacy format: {"skill": "proposal", "tool": "create_draft", "args": {...}}
	var legacy struct {
		Skill string          `json:"skill"`
		Tool  string          `json:"tool"`
		Args  json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(block), &legacy); err == nil && legacy.Skill != "" && legacy.Tool != "" {
		if isProposalTool(legacy.Tool) && legacy.Skill != "proposal" {
			legacy.Skill = "proposal"
		}
		argsStr := "{}"
		if len(legacy.Args) > 0 {
			argsStr = string(legacy.Args)
		}
		return legacy.Skill + "." + legacy.Tool, argsStr, true
	}

	return "", "", false
}

func isProposalTool(name string) bool {
	switch name {
	case "create_draft", "update_draft", "get_draft", "list_drafts", "submit", "status", "reviews", "vote":
		return true
	}
	return false
}

// handleChatMessage runs the full ReAct loop inside this sandbox (D2-a).
//
// The agent calls Ollama, parses tool-call blocks, and returns either an
// intermediate "tool_call" response (so the daemon can execute the tool and
// call back with the result appended) or a "final" response with the
// assistant's text.  The outer ReAct loop driver lives in the daemon
// (makeChatMessageHandler).
//
// When payload.StructuredOutput is true (Phase 0 enforcement), the agent
// calls Ollama with format="json" and expects a response of the form:
//
//	{"status":"tool_call","tool":"<name>","args":{…}}
//	{"status":"final","content":"<text>"}
//
// If the model returns invalid or missing JSON in structured mode, the
// response is retried once with an explicit correction prompt.
func handleChatMessage(ctx context.Context, req *Request) *Response {
	var payload ChatMessagePayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return errorResponse(req.ID, fmt.Sprintf("invalid chat.message payload: %v", err))
	}

	if payload.Model == "" {
		return errorResponse(req.ID, "model is required")
	}
	if len(payload.Messages) == 0 {
		return errorResponse(req.ID, "messages are required")
	}

	// Build the Ollama-compatible message list (strip the Name field for
	// non-tool roles so that Ollama models that don't support it don't choke).
	ollamaMsgs := buildOllamaMsgs(payload.Messages)

	callCtx, cancel := context.WithTimeout(ctx, ollamaTimeout)
	defer cancel()

	if payload.StructuredOutput {
		return handleChatMessageStructured(callCtx, req.ID, payload.Model, payload.StreamID, ollamaMsgs, payload.Temperature, payload.Seed)
	}

	content, thinking, nativeToolCalls, err := callOllamaViaProxy(callCtx, payload.Model, payload.StreamID, ollamaMsgs, "", buildOllamaOptions(payload.Temperature, payload.Seed, nil))
	if err != nil {
		return errorResponse(req.ID, fmt.Sprintf("ollama error: %v", err))
	}

	// Debug: log received tool calls.
	if len(nativeToolCalls) > 0 {
		toolNames := make([]string, len(nativeToolCalls))
		for i, tc := range nativeToolCalls {
			toolNames[i] = tc.Function.Name
		}
		log.Printf("[guest.handleChatMessage] received %d tool calls from proxy: %v", len(nativeToolCalls), toolNames)
	}

	// Prefer native tool calls when Ollama returns message.tool_calls with empty content.
	if toolName, argsJSON, ok := parseProxyToolCall(nativeToolCalls); ok {
		log.Printf("[guest.handleChatMessage] parsed tool call: %s", toolName)
		chatResp := ChatResponse{
			Status:   "tool_call",
			Thinking: strings.TrimSpace(thinking),
			Tool:     toolName,
			Args:     argsJSON,
		}
		data, _ := json.Marshal(chatResp)
		return &Response{ID: req.ID, Success: true, Data: data}
	}

	// Check for a tool-call block in the response.
	toolName, argsJSON, hasTool := parseAgentToolCall(content)
	if hasTool {
		chatResp := ChatResponse{
			Status:   "tool_call",
			Thinking: strings.TrimSpace(thinking),
			Tool:     toolName,
			Args:     argsJSON,
		}
		data, _ := json.Marshal(chatResp)
		return &Response{ID: req.ID, Success: true, Data: data}
	}

	// No tool call — return the final assistant response.
	chatResp := ChatResponse{
		Status:   "final",
		Role:     "assistant",
		Content:  content,
		Thinking: strings.TrimSpace(thinking),
	}
	data, _ := json.Marshal(chatResp)
	return &Response{ID: req.ID, Success: true, Data: data}
}

// structuredChatReply is the JSON schema Ollama is asked to produce when
// StructuredOutput is enabled.
type structuredChatReply struct {
	Status  string          `json:"status"`            // "final" | "tool_call"
	Content string          `json:"content,omitempty"` // when status=="final"
	Tool    string          `json:"tool,omitempty"`    // when status=="tool_call"
	Args    json.RawMessage `json:"args,omitempty"`    // when status=="tool_call"
}

func parseStructuredChatReply(content string) (structuredChatReply, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return structuredChatReply{}, false
	}

	var reply structuredChatReply
	if err := json.Unmarshal([]byte(content), &reply); err == nil && strings.TrimSpace(reply.Status) != "" {
		return reply, true
	}

	for _, marker := range []string{"```json", "```"} {
		start := strings.Index(content, marker)
		if start < 0 {
			continue
		}
		after := content[start+len(marker):]
		end := strings.Index(after, "```")
		if end < 0 {
			continue
		}
		block := strings.TrimSpace(after[:end])
		if err := json.Unmarshal([]byte(block), &reply); err == nil && strings.TrimSpace(reply.Status) != "" {
			return reply, true
		}
	}

	return structuredChatReply{}, false
}

func decodeStructuredChatResponse(content, thinking string, nativeToolCalls []proxyToolCall, allowPlainFinal bool) (ChatResponse, bool) {
	trimmedThinking := strings.TrimSpace(thinking)
	trimmedContent := strings.TrimSpace(content)

	if toolName, argsJSON, ok := parseProxyToolCall(nativeToolCalls); ok {
		return ChatResponse{
			Status:   "tool_call",
			Thinking: trimmedThinking,
			Tool:     toolName,
			Args:     argsJSON,
		}, true
	}

	if reply, ok := parseStructuredChatReply(trimmedContent); ok {
		switch reply.Status {
		case "tool_call":
			if strings.TrimSpace(reply.Tool) == "" {
				break
			}
			argsStr := "{}"
			if len(reply.Args) > 0 {
				argsStr = string(reply.Args)
			}
			return ChatResponse{
				Status:   "tool_call",
				Thinking: trimmedThinking,
				Tool:     reply.Tool,
				Args:     argsStr,
			}, true
		case "final":
			return ChatResponse{
				Status:   "final",
				Role:     "assistant",
				Content:  reply.Content,
				Thinking: trimmedThinking,
			}, true
		}
	}

	if toolName, argsJSON, ok := parseAgentToolCall(trimmedContent); ok {
		return ChatResponse{
			Status:   "tool_call",
			Thinking: trimmedThinking,
			Tool:     toolName,
			Args:     argsJSON,
		}, true
	}

	if allowPlainFinal && trimmedContent != "" {
		return ChatResponse{
			Status:   "final",
			Role:     "assistant",
			Content:  trimmedContent,
			Thinking: trimmedThinking,
		}, true
	}

	return ChatResponse{}, false
}

// handleChatMessageStructured drives the ReAct step with Ollama JSON mode
// (format="json") to achieve deterministic tool-call parsing.  On the first
// call the model is asked to produce a structuredChatReply; if parsing fails
// we retry once with an explicit correction prompt before giving up.
func handleChatMessageStructured(ctx context.Context, reqID, model, streamID string, msgs []map[string]string, temperature *float64, seed int64) *Response {
	const maxAttempts = 2
	options := buildOllamaOptions(temperature, seed, nil)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		content, thinking, nativeToolCalls, err := callOllamaViaProxy(ctx, model, streamID, msgs, "json", options)
		if err != nil {
			return errorResponse(reqID, fmt.Sprintf("ollama error: %v", err))
		}

		if chatResp, ok := decodeStructuredChatResponse(content, thinking, nativeToolCalls, attempt == maxAttempts-1); ok {
			data, _ := json.Marshal(chatResp)
			return &Response{ID: reqID, Success: true, Data: data}
		}

		// JSON was invalid or status field missing — add a correction prompt
		// for the next attempt.
		if attempt < maxAttempts-1 {
			msgs = append(msgs, map[string]string{
				"role":    "user",
				"content": structuredOutputCorrectionPrompt,
			})
		}
	}

	return errorResponse(reqID, "structured output enforcement: model did not return valid JSON after retries")
}

func buildOllamaOptions(temperature *float64, seed int64, defaultTemperature *float64) map[string]interface{} {
	var options map[string]interface{}
	add := func(key string, value interface{}) {
		if options == nil {
			options = make(map[string]interface{}, 2)
		}
		options[key] = value
	}
	if defaultTemperature != nil {
		add("temperature", *defaultTemperature)
	}
	if temperature != nil {
		add("temperature", *temperature)
	}
	if seed != 0 {
		add("seed", seed)
	}
	return options
}

// buildOllamaMsgs converts ChatMsg slice into the format Ollama expects.
// Tool-result messages use role "user" with a "[Tool X returned]: Y" prefix
// for models that don't support the "tool" role natively.
func buildOllamaMsgs(msgs []ChatMsg) []map[string]string {
	out := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "tool":
			name := m.Name
			if name == "" {
				name = "tool"
			}
			out = append(out, map[string]string{
				"role":    "user",
				"content": fmt.Sprintf("[Tool %s returned]: %s", name, m.Content),
			})
		default:
			out = append(out, map[string]string{
				"role":    m.Role,
				"content": m.Content,
			})
		}
	}
	return out
}

// callOllamaViaProxy sends an LLM inference request to the host-side OllamaProxy
// over vsock (AF_VSOCK, host CID 2, port 1025).  The proxy validates the model
// against the allowlist and proxies to the local Ollama service; this VM has no
// network interface and cannot reach Ollama directly.
type proxyToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"function"`
}

func parseProxyToolCall(toolCalls []proxyToolCall) (toolName, argsJSON string, found bool) {
	for _, call := range toolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}

		args := "{}"
		rawArgs := bytes.TrimSpace(call.Function.Arguments)
		if len(rawArgs) > 0 {
			var asString string
			if err := json.Unmarshal(rawArgs, &asString); err == nil {
				trimmed := strings.TrimSpace(asString)
				if trimmed != "" && json.Valid([]byte(trimmed)) {
					args = trimmed
				}
			} else if json.Valid(rawArgs) {
				args = string(rawArgs)
			}
		}
		return name, args, true
	}
	return "", "", false
}

func callOllamaViaProxy(ctx context.Context, model, streamID string, messages []map[string]string, format string, options map[string]interface{}) (string, string, []proxyToolCall, error) {
	// Dial host (CID 2) on the well-known LLM proxy port.
	const proxyPort = 1025
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return "", "", nil, fmt.Errorf("vsock socket: %w", err)
	}

	// Apply the context deadline as a socket-level timeout.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			unix.Close(fd)
			return "", "", nil, fmt.Errorf("context already expired")
		}
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv)
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	}

	sa := &unix.SockaddrVM{
		CID:  unix.VMADDR_CID_HOST,
		Port: proxyPort,
	}
	if err := unix.Connect(fd, sa); err != nil {
		unix.Close(fd)
		return "", "", nil, fmt.Errorf("vsock connect to llm proxy: %w", err)
	}

	// Wrap the raw fd in a net.Conn for JSON encode/decode.
	// net.FileConn can't handle AF_VSOCK (getsockname fails), so we wrap
	// the fd in an os.File and use it directly.
	file := os.NewFile(uintptr(fd), "vsock-proxy")
	conn := &vsockConn{file: file}
	defer conn.Close()

	reqID := fmt.Sprintf("%d", time.Now().UnixNano())
	proxyReq := struct {
		RequestID string                 `json:"request_id"`
		StreamID  string                 `json:"stream_id,omitempty"`
		Model     string                 `json:"model"`
		Messages  []map[string]string    `json:"messages"`
		Format    string                 `json:"format,omitempty"`
		Options   map[string]interface{} `json:"options,omitempty"`
	}{
		RequestID: reqID,
		StreamID:  streamID,
		Model:     model,
		Messages:  messages,
		Format:    format,
		Options:   options,
	}

	if err := json.NewEncoder(conn).Encode(proxyReq); err != nil {
		return "", "", nil, fmt.Errorf("send proxy request: %w", err)
	}

	var proxyResp struct {
		RequestID string          `json:"request_id"`
		Content   string          `json:"content,omitempty"`
		Thinking  string          `json:"thinking,omitempty"`
		ToolCalls []proxyToolCall `json:"tool_calls,omitempty"`
		Error     string          `json:"error,omitempty"`
	}
	if err := json.NewDecoder(conn).Decode(&proxyResp); err != nil {
		return "", "", nil, fmt.Errorf("read proxy response: %w", err)
	}
	if proxyResp.Error != "" {
		return "", "", nil, fmt.Errorf("llm proxy: %s", proxyResp.Error)
	}
	return proxyResp.Content, proxyResp.Thinking, proxyResp.ToolCalls, nil
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

// vsockConn wraps an os.File (backed by an AF_VSOCK fd) as a net.Conn.
// Go's net.FileConn doesn't support AF_VSOCK (getsockname fails), so
// we bypass it and use the file directly for Read/Write.
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

// vsockListener implements net.Listener over an AF_VSOCK file descriptor.
// Go's net.FileListener can't handle AF_VSOCK sockets (getsockname returns
// an address family that the net package doesn't understand), so we wrap the
// raw fd ourselves.
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
