package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// OllamaProxyVsockPort is the well-known vsock port the guest-agent connects to
// on the host (CID 2) for LLM inference.  When a guest running inside a
// Firecracker VM connects to VMADDR_CID_HOST:OllamaProxyVsockPort, Firecracker
// routes the connection to <vsock_device_path>_1025 on the host.
const OllamaProxyVsockPort = 1025

// MaxProxyPayloadBytes is the maximum request payload the proxy will accept.
// This prevents a compromised or misbehaving guest from causing resource
// exhaustion on the host.
const MaxProxyPayloadBytes = 256 * 1024 // 256 KB

// maxOllamaResponseBytes is the cap applied when reading raw Ollama HTTP
// responses (error bodies and thinking-fallback bodies).  Ollama response
// bodies for chat completions are read incrementally via streaming; this cap
// only applies to the non-streaming paths (error responses and the thinking
// fallback endpoint which buffers the full response before parsing).
// 8 MiB is generous enough for any realistic model reasoning output while
// preventing an OOM from a misbehaving local Ollama instance.
const maxOllamaResponseBytes = 8 * 1024 * 1024 // 8 MiB

// ProxyRequest is the vsock request from a guest agent to the host LLM proxy.
type ProxyRequest struct {
	RequestID string                 `json:"request_id"`
	StreamID  string                 `json:"stream_id,omitempty"`
	Model     string                 `json:"model"`
	Messages  []map[string]string    `json:"messages"`
	Format    string                 `json:"format,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
}

// ProxyResponse is the host's reply to a guest ProxyRequest.
type ProxyResponse struct {
	RequestID string          `json:"request_id"`
	Content   string          `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ToolCalls []ProxyToolCall `json:"tool_calls,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// ProxyToolCall mirrors Ollama's native tool-call shape so it can be
// propagated over vsock to the guest agent.
type ProxyToolCall struct {
	ID       string            `json:"id,omitempty"`
	Function ProxyToolFunction `json:"function"`
}

type ProxyToolFunction struct {
	Index     int             `json:"index,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ChatProgressSnapshot holds the current streamed state for an in-flight chat request.
type ChatProgressSnapshot struct {
	StreamID  string    `json:"stream_id"`
	RequestID string    `json:"request_id,omitempty"`
	Model     string    `json:"model,omitempty"`
	Thinking  string    `json:"thinking,omitempty"`
	Content   string    `json:"content,omitempty"`
	Done      bool      `json:"done,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

var chatProgressStore = struct {
	mu    sync.RWMutex
	items map[string]ChatProgressSnapshot
}{
	items: make(map[string]ChatProgressSnapshot),
}

const chatProgressRetention = 15 * time.Minute

func trimChatProgressLocked(now time.Time) {
	for streamID, snapshot := range chatProgressStore.items {
		if now.Sub(snapshot.UpdatedAt) > chatProgressRetention {
			delete(chatProgressStore.items, streamID)
		}
	}
}

func initChatProgress(streamID, requestID, model string) {
	if strings.TrimSpace(streamID) == "" {
		return
	}
	now := time.Now().UTC()
	chatProgressStore.mu.Lock()
	defer chatProgressStore.mu.Unlock()
	trimChatProgressLocked(now)
	chatProgressStore.items[streamID] = ChatProgressSnapshot{
		StreamID:  streamID,
		RequestID: requestID,
		Model:     model,
		UpdatedAt: now,
	}
}

func appendChatProgress(streamID, thinkingDelta, contentDelta string) {
	if strings.TrimSpace(streamID) == "" {
		return
	}
	now := time.Now().UTC()
	chatProgressStore.mu.Lock()
	defer chatProgressStore.mu.Unlock()
	trimChatProgressLocked(now)
	snapshot := chatProgressStore.items[streamID]
	if snapshot.StreamID == "" {
		snapshot.StreamID = streamID
	}
	if thinkingDelta != "" {
		snapshot.Thinking += thinkingDelta
	}
	if contentDelta != "" {
		snapshot.Content += contentDelta
	}
	snapshot.UpdatedAt = now
	chatProgressStore.items[streamID] = snapshot
}

func completeChatProgress(streamID string, content string, thinking string, err error) {
	if strings.TrimSpace(streamID) == "" {
		return
	}
	now := time.Now().UTC()
	chatProgressStore.mu.Lock()
	defer chatProgressStore.mu.Unlock()
	trimChatProgressLocked(now)
	snapshot := chatProgressStore.items[streamID]
	if snapshot.StreamID == "" {
		snapshot.StreamID = streamID
	}
	if content != "" {
		snapshot.Content = content
	}
	if thinking != "" {
		snapshot.Thinking = thinking
	}
	snapshot.Done = true
	if err != nil {
		snapshot.Error = err.Error()
	}
	snapshot.UpdatedAt = now
	chatProgressStore.items[streamID] = snapshot
}

// GetChatProgress returns the latest progress snapshot for a stream ID.
func GetChatProgress(streamID string) (ChatProgressSnapshot, bool) {
	if strings.TrimSpace(streamID) == "" {
		return ChatProgressSnapshot{}, false
	}
	now := time.Now().UTC()
	chatProgressStore.mu.Lock()
	defer chatProgressStore.mu.Unlock()
	trimChatProgressLocked(now)
	snapshot, ok := chatProgressStore.items[streamID]
	return snapshot, ok
}

// OllamaProxy listens on per-VM vsock UDS paths and proxies LLM inference
// requests to the local Ollama service.  VMs that use this proxy require no
// network interface — all LLM access flows through the vsock kernel channel.
//
// Security properties:
//   - Listens only on per-VM <vsock_path>_1025 sockets; inaccessible outside the VM channel.
//   - Validates every model name against a compile-time allowlist.
//   - Enforces a hard per-request payload cap (MaxProxyPayloadBytes).
//   - Audit-logs every inference call via the kernel tamper-evident log.
//   - Ollama is called on host loopback only; no traffic leaves the machine.
type OllamaProxy struct {
	allowedModels map[string]bool
	ollamaURL     string
	httpClient    *http.Client
	kern          *kernel.Kernel
	logger        *zap.Logger

	unsupportedThinkingModels map[string]bool

	mu        sync.Mutex
	listeners map[string]net.Listener // vmID -> UDS listener on vsock.sock_1025
}

// NewOllamaProxy creates a proxy whose allowedModels list is enforced on every
// request.  ollamaURL defaults to the standard local endpoint if empty.
func NewOllamaProxy(allowedModels []string, ollamaURL string, kern *kernel.Kernel, logger *zap.Logger) *OllamaProxy {
	return NewOllamaProxyWithHTTPClient(allowedModels, ollamaURL, nil, kern, logger)
}

// NewOllamaProxyWithHTTPClient is the test seam for replaying recorded Ollama
// traffic without changing production behavior.
func NewOllamaProxyWithHTTPClient(allowedModels []string, ollamaURL string, httpClient *http.Client, kern *kernel.Kernel, logger *zap.Logger) *OllamaProxy {
	if ollamaURL == "" {
		ollamaURL = OllamaEndpoint + "/api/chat"
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	m := make(map[string]bool, len(allowedModels))
	for _, name := range allowedModels {
		m[name] = true
	}
	return &OllamaProxy{
		allowedModels:             m,
		ollamaURL:                 ollamaURL,
		httpClient:                httpClient,
		kern:                      kern,
		logger:                    logger,
		unsupportedThinkingModels: make(map[string]bool),
		listeners:                 make(map[string]net.Listener),
	}
}

func (p *OllamaProxy) isThinkingUnsupported(model string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.unsupportedThinkingModels[model]
}

func (p *OllamaProxy) markThinkingUnsupported(model string) {
	if strings.TrimSpace(model) == "" {
		return
	}
	p.mu.Lock()
	p.unsupportedThinkingModels[model] = true
	p.mu.Unlock()
}

// AllowedModelsFromRegistry returns model names from KnownGoodModels, suitable
// for passing directly to NewOllamaProxy.
func AllowedModelsFromRegistry() []string {
	names := make([]string, len(KnownGoodModels))
	for i, m := range KnownGoodModels {
		names[i] = m.Name
	}
	return names
}

// StartForVM starts the LLM proxy for a specific VM.  vsockPath is the
// Firecracker vsock device socket (e.g. /run/aegisclaw/.../vsock.sock).
// The proxy binds to vsockPath + "_1025", which is where Firecracker delivers
// guest-initiated connections to host CID 2 port 1025.
//
// This method must be called after the VM starts (so the chroot directory
// exists) and before the guest is asked to perform any LLM inference.
func (p *OllamaProxy) StartForVM(vmID, vsockPath string) error {
	listenPath := fmt.Sprintf("%s_%d", vsockPath, OllamaProxyVsockPort)

	// Remove any stale socket left by a prior crash.
	_ = os.Remove(listenPath)

	l, err := net.Listen("unix", listenPath)
	if err != nil {
		return fmt.Errorf("llm proxy: listen for vm %s at %s: %w", vmID, listenPath, err)
	}
	// The jailed Firecracker process runs as a sandbox-specific UID/GID and
	// needs write permission to connect to this socket.  Go's net.Listen
	// applies the process umask which may strip the world-write bit.
	_ = os.Chmod(listenPath, 0666)

	p.mu.Lock()
	p.listeners[vmID] = l
	p.mu.Unlock()

	go p.serveVM(vmID, l)

	p.logger.Info("llm proxy started for vm",
		zap.String("vm_id", vmID),
		zap.String("socket", listenPath),
	)
	return nil
}

// StopForVM closes the proxy listener for the specified VM.
func (p *OllamaProxy) StopForVM(vmID string) {
	p.mu.Lock()
	l, ok := p.listeners[vmID]
	if ok {
		delete(p.listeners, vmID)
	}
	p.mu.Unlock()

	if ok {
		l.Close()
		p.logger.Info("llm proxy stopped for vm", zap.String("vm_id", vmID))
	}
}

// Stop closes every active proxy listener.
func (p *OllamaProxy) Stop() {
	p.mu.Lock()
	ls := make([]net.Listener, 0, len(p.listeners))
	for _, l := range p.listeners {
		ls = append(ls, l)
	}
	p.listeners = make(map[string]net.Listener)
	p.mu.Unlock()

	for _, l := range ls {
		l.Close()
	}
}

func (p *OllamaProxy) serveVM(vmID string, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return // listener closed; exit silently
		}
		go p.handleConn(vmID, conn)
	}
}

func (p *OllamaProxy) handleConn(vmID string, conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(300 * time.Second))

	// Guard against oversized payloads before decoding.
	limited := io.LimitReader(conn, MaxProxyPayloadBytes+1)
	var req ProxyRequest
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(ProxyResponse{Error: "decode: " + err.Error()})
		return
	}

	resp := p.handleRequest(vmID, &req)
	_ = json.NewEncoder(conn).Encode(resp)
}

// isModelAllowed checks whether the requested model is in the allowlist.
// Model names from Ollama often carry a ":tag" suffix (e.g. "mistral-nemo:latest")
// while the registry stores bare names (e.g. "mistral-nemo").  Both forms are
// checked so the allowlist works regardless of how the persona file specifies
// the model name.
func (p *OllamaProxy) isModelAllowed(model string) bool {
	if p.allowedModels[model] {
		return true
	}
	// Strip tag suffix and try again: "mistral-nemo:latest" → "mistral-nemo"
	for i, c := range model {
		if c == ':' {
			return p.allowedModels[model[:i]]
		}
	}
	return false
}

func decodeOllamaChatBody(body io.Reader, onChunk func(contentDelta, thinkingDelta string)) (string, string, []ProxyToolCall, error) {
	dec := json.NewDecoder(body)
	var content strings.Builder
	var thinking strings.Builder
	var toolCalls []ProxyToolCall
	decoded := 0

	for {
		var chunk struct {
			Error     string `json:"error,omitempty"`
			Thinking  string `json:"thinking,omitempty"`
			Reasoning string `json:"reasoning,omitempty"`
			Message   struct {
				Content   string          `json:"content"`
				Thinking  string          `json:"thinking,omitempty"`
				Reasoning string          `json:"reasoning,omitempty"`
				ToolCalls []ProxyToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
		}
		err := dec.Decode(&chunk)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", nil, err
		}
		decoded++
		if strings.TrimSpace(chunk.Error) != "" {
			return "", "", nil, fmt.Errorf("ollama error: %s", chunk.Error)
		}
		// Accumulate tool calls across all chunks; only use them if none were captured yet.
		// Ollama typically emits tool_calls in the first chunk with empty content.
		if len(toolCalls) == 0 && len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}
		contentDelta := chunk.Message.Content
		thinkingDelta := chunk.Message.Thinking
		if thinkingDelta == "" {
			thinkingDelta = chunk.Message.Reasoning
		}
		if thinkingDelta == "" {
			thinkingDelta = chunk.Thinking
		}
		if thinkingDelta == "" {
			thinkingDelta = chunk.Reasoning
		}
		if chunk.Message.Content != "" {
			content.WriteString(chunk.Message.Content)
		}
		if chunk.Message.Thinking != "" {
			thinking.WriteString(chunk.Message.Thinking)
		}
		if chunk.Message.Reasoning != "" {
			thinking.WriteString(chunk.Message.Reasoning)
		}
		if chunk.Thinking != "" {
			thinking.WriteString(chunk.Thinking)
		}
		if chunk.Reasoning != "" {
			thinking.WriteString(chunk.Reasoning)
		}
		if onChunk != nil && (contentDelta != "" || thinkingDelta != "") {
			onChunk(contentDelta, thinkingDelta)
		}
	}

	if decoded == 0 {
		return "", "", nil, fmt.Errorf("empty response body")
	}

	contentText := content.String()
	thinkingText := strings.TrimSpace(thinking.String())

	// Debug: log tool calls captured for tracing.
	if len(toolCalls) > 0 {
		toolNames := make([]string, len(toolCalls))
		for i, tc := range toolCalls {
			toolNames[i] = tc.Function.Name
		}
		// Note: this proxy instance doesn't have logger visible here,  so we'll skip this debug log.
		// The tool calls will be visible when returned to the guest in that log.
	}

	// Some models expose reasoning inline in content using think or thought tags.
	// Extract both <think>...</think> and <thought>...</thought> patterns.
	if thinkingText == "" {
		lower := strings.ToLower(contentText)

		// Try <thought>...</thought> first (gemma4 uses this).
		startThought := strings.Index(lower, "<thought>")
		endThought := strings.Index(lower, "</thought>")

		// Fall back to <think>...</think> if thought tags not found.
		startThink := strings.Index(lower, "<think>")
		endThink := strings.Index(lower, "</think>")

		// Prefer whichever appears first in the content.
		if startThought >= 0 && endThought > startThought {
			if startThink < 0 || startThought < startThink {
				// Use thought tag extraction.
				thinkingText = strings.TrimSpace(contentText[startThought+len("<thought>") : endThought])
				contentText = strings.TrimSpace(contentText[:startThought] + contentText[endThought+len("</thought>"):])
			} else {
				// Use think tag extraction (appears first).
				thinkingText = strings.TrimSpace(contentText[startThink+len("<think>") : endThink])
				contentText = strings.TrimSpace(contentText[:startThink] + contentText[endThink+len("</think>"):])
			}
		} else if startThink >= 0 && endThink > startThink {
			// Use think tag extraction.
			thinkingText = strings.TrimSpace(contentText[startThink+len("<think>") : endThink])
			contentText = strings.TrimSpace(contentText[:startThink] + contentText[endThink+len("</think>"):])
		}
	}

	// Clean up any orphaned opening thought/think tags left in content
	// (tags without closing pairs, or incomplete tags at boundaries).
	contentText = stripOrphanedThinkingTags(contentText)

	return contentText, thinkingText, toolCalls, nil
}

func stripOrphanedThinkingTags(content string) string {
	lower := strings.ToLower(content)

	// Remove leading <thought or <think if they appear to be incomplete or orphaned.
	for _, marker := range []string{"<thought", "<think"} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			// Check if there's a closing tag after this.
			closing := ""
			if marker == "<thought" {
				closing = "</thought>"
			} else {
				closing = "</think>"
			}
			closingIdx := strings.Index(lower[idx:], closing)
			if closingIdx < 0 {
				// No closing tag found — this is orphaned. Remove it and everything after.
				content = strings.TrimSpace(content[:idx])
				lower = strings.ToLower(content)
			}
		}
	}
	return content

}

func fallbackThinkingMessages(messages []map[string]string) []map[string]string {
	if len(messages) == 0 {
		return nil
	}

	// Preserve platform identity and current task context by keeping the first
	// system message (if any) plus a short tail of recent turns.
	var out []map[string]string
	for _, m := range messages {
		if strings.EqualFold(strings.TrimSpace(m["role"]), "system") {
			out = append(out, map[string]string{
				"role":    "system",
				"content": m["content"],
			})
			break
		}
	}

	const tailMessages = 8
	start := len(messages) - tailMessages
	if start < 0 {
		start = 0
	}
	for i := start; i < len(messages); i++ {
		role := strings.TrimSpace(messages[i]["role"])
		if role == "" || strings.EqualFold(role, "system") {
			continue
		}
		out = append(out, map[string]string{
			"role":    role,
			"content": messages[i]["content"],
		})
	}

	return out
}

func (p *OllamaProxy) fetchFallbackThinking(req *ProxyRequest) (string, error) {
	if p.isThinkingUnsupported(req.Model) {
		return "", nil
	}

	fallbackMsgs := fallbackThinkingMessages(req.Messages)
	if len(fallbackMsgs) == 0 {
		return "", nil
	}

	// Keep fallback compact to reduce latency: ask for reasoning only, but keep
	// the same system identity and recent dialogue context.
	fallbackReq := map[string]interface{}{
		"model": req.Model,
		"messages": append(fallbackMsgs, map[string]string{
			"role":    "user",
			"content": "Reasoning-only pass: provide concise internal reasoning in the thinking channel. Avoid long final output.",
		}),
		"stream": true,
		"think":  "high",
	}

	body, err := json.Marshal(fallbackReq)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.ollamaURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(httpResp.Body, maxOllamaResponseBytes))
	if err != nil {
		return "", err
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		excerpt := strings.ToLower(strings.TrimSpace(string(bodyBytes)))
		if httpResp.StatusCode == http.StatusBadRequest && strings.Contains(excerpt, "does not support thinking") {
			p.markThinkingUnsupported(req.Model)
			return "", nil
		}
		return "", fmt.Errorf("fallback ollama http %d", httpResp.StatusCode)
	}

	_, thinking, _, err := decodeOllamaChatBody(bytes.NewReader(bodyBytes), nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(thinking), nil
}

func (p *OllamaProxy) handleRequest(vmID string, req *ProxyRequest) ProxyResponse {
	initChatProgress(req.StreamID, req.RequestID, req.Model)
	finalizeProgress := func(content string, thinking string, err error) {
		completeChatProgress(req.StreamID, content, thinking, err)
	}

	// Enforce model allowlist — this is the primary security gate.
	if !p.isModelAllowed(req.Model) {
		p.logger.Warn("llm proxy: blocked disallowed model",
			zap.String("vm_id", vmID),
			zap.String("model", req.Model),
			zap.Any("allowlist", p.allowedModels),
		)
		err := fmt.Errorf("model %q is not in the approved allowlist", req.Model)
		finalizeProgress("", "", err)
		return ProxyResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		}
	}

	// Build the Ollama /api/chat request.
	// For structured JSON calls we prefer non-streaming and no explicit thinking
	// effort so recordings stay compact and deterministic.
	structuredJSON := strings.EqualFold(strings.TrimSpace(req.Format), "json")
	ollamaReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   !structuredJSON,
	}
	if !structuredJSON && !p.isThinkingUnsupported(req.Model) {
		// Conversational turns benefit from richer reasoning and incremental
		// progress updates.
		ollamaReq["think"] = "high"
	}
	if req.Format != "" {
		ollamaReq["format"] = req.Format
	}
	if len(req.Options) > 0 {
		ollamaReq["options"] = req.Options
	}

	// Allow up to 5 minutes: thinking models may take longer for deep
	// reasoning before producing their first output token.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var httpResp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		body, err := json.Marshal(ollamaReq)
		if err != nil {
			finalizeProgress("", "", fmt.Errorf("marshal: %w", err))
			return ProxyResponse{RequestID: req.RequestID, Error: "marshal: " + err.Error()}
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.ollamaURL, bytes.NewReader(body))
		if err != nil {
			finalizeProgress("", "", fmt.Errorf("build request: %w", err))
			return ProxyResponse{RequestID: req.RequestID, Error: "build request: " + err.Error()}
		}
		httpReq.Header.Set("Content-Type", "application/json")

		httpResp, err = p.httpClient.Do(httpReq)
		if err != nil {
			finalizeProgress("", "", fmt.Errorf("ollama: %w", err))
			return ProxyResponse{RequestID: req.RequestID, Error: "ollama: " + err.Error()}
		}

		if httpResp.StatusCode >= http.StatusOK && httpResp.StatusCode < http.StatusMultipleChoices {
			break
		}

		bodyBytes, err := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			finalizeProgress("", "", fmt.Errorf("read response: %w", err))
			return ProxyResponse{RequestID: req.RequestID, Error: "read response: " + err.Error()}
		}

		excerpt := strings.TrimSpace(string(bodyBytes))
		if len(excerpt) > 800 {
			excerpt = excerpt[:800] + "...[truncated]"
		}

		if attempt == 0 && httpResp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(excerpt), "does not support thinking") {
			p.markThinkingUnsupported(req.Model)
			delete(ollamaReq, "think")
			p.logger.Info("llm proxy: retrying request without think parameter",
				zap.String("model", req.Model),
			)
			continue
		}

		err = fmt.Errorf("ollama http %d: %s", httpResp.StatusCode, excerpt)
		finalizeProgress("", "", err)
		return ProxyResponse{RequestID: req.RequestID, Error: fmt.Sprintf("ollama http %d: %s", httpResp.StatusCode, excerpt)}
	}
	defer httpResp.Body.Close()

	var rawBody bytes.Buffer
	content, thinking, toolCalls, err := decodeOllamaChatBody(io.TeeReader(httpResp.Body, &rawBody), func(contentDelta, thinkingDelta string) {
		appendChatProgress(req.StreamID, thinkingDelta, contentDelta)
	})
	if err != nil {
		finalizeProgress("", "", fmt.Errorf("decode response: %w", err))
		return ProxyResponse{RequestID: req.RequestID, Error: "decode response: " + err.Error()}
	}
	if strings.TrimSpace(thinking) == "" {
		if structuredJSON {
			// Structured JSON review calls prioritize deterministic parsed output;
			// issuing an extra reasoning-only call adds latency and cassette noise.
			p.logger.Debug("llm proxy: skipping fallback thinking for structured json request",
				zap.String("model", req.Model),
			)
		} else {
			// For conversational chat traffic, a second reasoning-only request adds
			// significant latency/token cost without changing the final answer.
			// Keep the primary response content and continue when thinking is empty.
			p.logger.Debug("llm proxy: empty thinking in Ollama response; skipping fallback",
				zap.Int("message_count", len(req.Messages)),
				zap.String("model", req.Model),
			)
		}
	}

	// Audit-log this inference call so every LLM invocation is in the
	// tamper-evident chain, associated with the requesting VM.
	// Include a capped excerpt of the model's thinking so the reasoning
	// used by the agent is traceable in the audit trail.
	thinkingExcerpt := thinking
	if len(thinkingExcerpt) > 800 {
		thinkingExcerpt = thinkingExcerpt[:800] + "...[truncated]"
	}
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"vm_id":    vmID,
		"model":    req.Model,
		"thinking": thinkingExcerpt,
	})
	action := kernel.NewAction(kernel.ActionLLMInfer, "llm-proxy", auditPayload)
	if _, err := p.kern.SignAndLog(action); err != nil {
		p.logger.Warn("llm proxy: audit log failed", zap.Error(err))
	}

	finalizeProgress(content, thinking, nil)

	return ProxyResponse{
		RequestID: req.RequestID,
		Content:   content,
		Thinking:  thinking,
		ToolCalls: toolCalls,
	}
}
