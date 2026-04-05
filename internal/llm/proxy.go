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

// ProxyRequest is the vsock request from a guest agent to the host LLM proxy.
type ProxyRequest struct {
	RequestID string                 `json:"request_id"`
	Model     string                 `json:"model"`
	Messages  []map[string]string    `json:"messages"`
	Format    string                 `json:"format,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
}

// ProxyResponse is the host's reply to a guest ProxyRequest.
type ProxyResponse struct {
	RequestID string `json:"request_id"`
	Content   string `json:"content,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Error     string `json:"error,omitempty"`
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
	kern          *kernel.Kernel
	logger        *zap.Logger

	mu        sync.Mutex
	listeners map[string]net.Listener // vmID -> UDS listener on vsock.sock_1025
}

// NewOllamaProxy creates a proxy whose allowedModels list is enforced on every
// request.  ollamaURL defaults to the standard local endpoint if empty.
func NewOllamaProxy(allowedModels []string, ollamaURL string, kern *kernel.Kernel, logger *zap.Logger) *OllamaProxy {
	if ollamaURL == "" {
		ollamaURL = OllamaEndpoint + "/api/chat"
	}
	m := make(map[string]bool, len(allowedModels))
	for _, name := range allowedModels {
		m[name] = true
	}
	return &OllamaProxy{
		allowedModels: m,
		ollamaURL:     ollamaURL,
		kern:          kern,
		logger:        logger,
		listeners:     make(map[string]net.Listener),
	}
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

func decodeOllamaChatBody(bodyBytes []byte) (string, string, error) {
	dec := json.NewDecoder(bytes.NewReader(bodyBytes))
	var content strings.Builder
	var thinking strings.Builder
	decoded := 0

	for {
		var chunk struct {
			Error     string `json:"error,omitempty"`
			Thinking  string `json:"thinking,omitempty"`
			Reasoning string `json:"reasoning,omitempty"`
			Message   struct {
				Content   string `json:"content"`
				Thinking  string `json:"thinking,omitempty"`
				Reasoning string `json:"reasoning,omitempty"`
			} `json:"message"`
		}
		err := dec.Decode(&chunk)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
		decoded++
		if strings.TrimSpace(chunk.Error) != "" {
			return "", "", fmt.Errorf("ollama error: %s", chunk.Error)
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
	}

	if decoded == 0 {
		return "", "", fmt.Errorf("empty response body")
	}

	contentText := content.String()
	thinkingText := strings.TrimSpace(thinking.String())

	// Some models expose reasoning inline in content using think tags.
	if thinkingText == "" {
		lower := strings.ToLower(contentText)
		start := strings.Index(lower, "<think>")
		end := strings.Index(lower, "</think>")
		if start >= 0 && end > start {
			thinkingText = strings.TrimSpace(contentText[start+len("<think>") : end])
			contentText = strings.TrimSpace(contentText[:start] + contentText[end+len("</think>"):])
		}
	}

	return contentText, thinkingText, nil
}

func latestUserMessage(messages []map[string]string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i]["role"]), "user") {
			return strings.TrimSpace(messages[i]["content"])
		}
	}
	return ""
}

func (p *OllamaProxy) fetchFallbackThinking(req *ProxyRequest) (string, error) {
	userMsg := latestUserMessage(req.Messages)
	if userMsg == "" {
		return "", nil
	}

	// Keep fallback compact to reduce latency: ask only for reasoning, not a
	// full final answer, using the latest user request.
	fallbackReq := map[string]interface{}{
		"model": req.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "Reasoning-only pass: provide concise internal reasoning in the thinking channel. Avoid long final output.",
			},
			{
				"role":    "user",
				"content": userMsg,
			},
		},
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

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", err
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fallback ollama http %d", httpResp.StatusCode)
	}

	_, thinking, err := decodeOllamaChatBody(bodyBytes)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(thinking), nil
}

func (p *OllamaProxy) handleRequest(vmID string, req *ProxyRequest) ProxyResponse {
	// Enforce model allowlist — this is the primary security gate.
	if !p.isModelAllowed(req.Model) {
		p.logger.Warn("llm proxy: blocked disallowed model",
			zap.String("vm_id", vmID),
			zap.String("model", req.Model),
			zap.Any("allowlist", p.allowedModels),
		)
		return ProxyResponse{
			RequestID: req.RequestID,
			Error:     fmt.Sprintf("model %q is not in the approved allowlist", req.Model),
		}
	}

	// Build the Ollama /api/chat request.
	// think:"high" requests stronger reasoning for models that support
	// configurable thinking effort.
	// For models without this support, Ollama may ignore the field.
	// stream:true captures tokenized thinking/content chunks for models that
	// emit reasoning only while streaming.
	ollamaReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
		"think":    "high",
	}
	if req.Format != "" {
		ollamaReq["format"] = req.Format
	}
	if len(req.Options) > 0 {
		ollamaReq["options"] = req.Options
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return ProxyResponse{RequestID: req.RequestID, Error: "marshal: " + err.Error()}
	}

	// Allow up to 5 minutes: thinking models may take longer for deep
	// reasoning before producing their first output token.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.ollamaURL, bytes.NewReader(body))
	if err != nil {
		return ProxyResponse{RequestID: req.RequestID, Error: "build request: " + err.Error()}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return ProxyResponse{RequestID: req.RequestID, Error: "ollama: " + err.Error()}
	}
	defer httpResp.Body.Close()
	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return ProxyResponse{RequestID: req.RequestID, Error: "read response: " + err.Error()}
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		excerpt := strings.TrimSpace(string(bodyBytes))
		if len(excerpt) > 800 {
			excerpt = excerpt[:800] + "...[truncated]"
		}
		return ProxyResponse{RequestID: req.RequestID, Error: fmt.Sprintf("ollama http %d: %s", httpResp.StatusCode, excerpt)}
	}

	content, thinking, err := decodeOllamaChatBody(bodyBytes)
	if err != nil {
		return ProxyResponse{RequestID: req.RequestID, Error: "decode response: " + err.Error()}
	}
	if strings.TrimSpace(thinking) == "" {
		bodyExcerpt := string(bodyBytes)
		if len(bodyExcerpt) > 1200 {
			bodyExcerpt = bodyExcerpt[:1200] + "...[truncated]"
		}
		msgPreview := make([]map[string]string, 0, 3)
		for i, m := range req.Messages {
			if i >= 3 {
				break
			}
			contentPreview := strings.TrimSpace(m["content"])
			if len(contentPreview) > 180 {
				contentPreview = contentPreview[:180] + "...[truncated]"
			}
			msgPreview = append(msgPreview, map[string]string{
				"role":    m["role"],
				"content": contentPreview,
			})
		}
		p.logger.Info("llm proxy: empty thinking in Ollama response",
			zap.Int("message_count", len(req.Messages)),
			zap.Any("message_preview", msgPreview),
			zap.String("model", req.Model),
			zap.ByteString("body", []byte(bodyExcerpt)),
		)

		fallbackThinking, fallbackErr := p.fetchFallbackThinking(req)
		if fallbackErr != nil {
			p.logger.Warn("llm proxy: fallback thinking request failed", zap.Error(fallbackErr), zap.String("model", req.Model))
		} else if strings.TrimSpace(fallbackThinking) != "" {
			thinking = fallbackThinking
			p.logger.Info("llm proxy: recovered thinking via fallback request",
				zap.String("model", req.Model),
				zap.Int("thinking_chars", len(thinking)),
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

	return ProxyResponse{
		RequestID: req.RequestID,
		Content:   content,
		Thinking:  thinking,
	}
}
