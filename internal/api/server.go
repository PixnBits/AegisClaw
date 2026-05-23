package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	aegispaths "github.com/PixnBits/AegisClaw/internal/paths"
	"go.uber.org/zap"
)

type peerUIDContextKey struct{}
type trustedCallerContextKey struct{}

// PeerUIDFromContext returns the Unix peer UID when the request arrived via the
// daemon's Unix socket transport.
func PeerUIDFromContext(ctx context.Context) (int, bool) {
	v := ctx.Value(peerUIDContextKey{})
	uid, ok := v.(int)
	return uid, ok
}

// WithTrustedCaller marks a context as originating from a trusted in-process
// caller (for example daemon-owned portal bridges) when no socket peer UID
// exists.
func WithTrustedCaller(ctx context.Context) context.Context {
	return context.WithValue(ctx, trustedCallerContextKey{}, true)
}

// IsTrustedCaller reports whether the context is explicitly marked as trusted.
func IsTrustedCaller(ctx context.Context) bool {
	v := ctx.Value(trustedCallerContextKey{})
	trusted, ok := v.(bool)
	return ok && trusted
}

// Handler processes an API action and returns a response.
type Handler func(ctx context.Context, data json.RawMessage) *Response

// Request is the envelope sent by the CLI client.
type Request struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Response is the envelope returned by the daemon.
type Response struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// CourtReviewRequest carries the payload for the "court.review" action.
type CourtReviewRequest struct {
	ProposalID   string          `json:"proposal_id"`
	ProposalData json.RawMessage `json:"proposal_data,omitempty"`
}

// CourtVoteRequest carries the payload for the "court.vote" action.
type CourtVoteRequest struct {
	ProposalID   string          `json:"proposal_id"`
	Voter        string          `json:"voter"`
	Approve      bool            `json:"approve"`
	Reason       string          `json:"reason"`
	ProposalData json.RawMessage `json:"proposal_data,omitempty"`
}

// SkillActivateRequest carries the payload for the "skill.activate" action.
type SkillActivateRequest struct {
	Name string `json:"name"`
}

// SkillInvokeRequest carries the payload for the "skill.invoke" action.
type SkillInvokeRequest struct {
	Skill string `json:"skill"`
	Tool  string `json:"tool"`
	Args string `json:"args,omitempty"`
}

// SkillDeactivateRequest carries the payload for the "skill.deactivate" action.
type SkillDeactivateRequest struct {
	Name string `json:"name"`
}

// ChatMessageRequest carries the payload for the "chat.message" action (D2).
// The CLI sends user input and conversation history; the daemon handles LLM
// interaction inside a sandboxed agent boundary.
type ChatMessageRequest struct {
	Input    string            `json:"input"`
	History  []ChatHistoryItem `json:"history,omitempty"`
	StreamID string            `json:"stream_id,omitempty"`
	// SessionID is an optional stable identifier for this conversation.
	// When provided it is used for session routing (sessions_list, sessions_history,
	// sessions_send, sessions_spawn).  If empty the daemon uses StreamID as a
	// best-effort session key.
	SessionID string `json:"session_id,omitempty"`
}

// ChatHistoryItem is a single message in the conversation history.
type ChatHistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatMessageResponse is the daemon's response to a chat.message request.
type ChatMessageResponse struct {
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Model     string          `json:"model,omitempty"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
	Thinking  json.RawMessage `json:"thinking_trace,omitempty"`
}

// ChatSlashRequest carries the payload for the "chat.slash" action (D2).
type ChatSlashRequest struct {
	Command string `json:"command"`
}

// ChatToolExecRequest carries the payload for the "chat.tool" action (D2).
type ChatToolExecRequest struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

// ChatSummarizeRequest carries the payload for the "chat.summarize" action (D2).
type ChatSummarizeRequest struct {
	ToolName   string            `json:"tool_name"`
	ToolResult string            `json:"tool_result"`
	History    []ChatHistoryItem `json:"history,omitempty"`
}

// VaultSecretAddRequest carries the payload for the "vault.secret.add" action.
// Value is the raw plaintext; the daemon encrypts it before writing to disk.
// Used for both creating a new secret and rotating (overwriting) an existing one.
type VaultSecretAddRequest struct {
	Name    string `json:"name"`
	SkillID string `json:"skill_id"`
	Value   string `json:"value"`
	Rotate  bool   `json:"rotate,omitempty"` // if true, secret must already exist
}

// VaultSecretListRequest carries the optional filter for the "vault.secret.list" action.
type VaultSecretListRequest struct {
	SkillID string `json:"skill_id,omitempty"` // if non-empty, filter by skill
}

// VaultSecretEntry is the metadata returned by the "vault.secret.list" action.
// The plaintext value is never included.
type VaultSecretEntry struct {
	Name      string `json:"name"`
	SkillID   string `json:"skill_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Size      int    `json:"size"`
}

// VaultSecretDeleteRequest carries the payload for the "vault.secret.delete" action.
type VaultSecretDeleteRequest struct {
	Name string `json:"name"`
}

// DefaultSocketPath returns the security-conscious default daemon socket path.
func DefaultSocketPath() string {
	path, err := aegispaths.DefaultSocketPath()
	if err != nil {
		return filepath.Join(os.TempDir(), "aegis", "daemon.sock")
	}
	return path
}

const defaultMaxAPIBodyBytes = 4 << 20

type unixRateWindow struct {
	second int64
	n      int
}

// Server listens on a Unix socket and dispatches incoming requests to
// registered handlers keyed by action name.
type Server struct {
	socketPath string
	logger     *zap.Logger
	listener   net.Listener
	mu         sync.RWMutex
	handlers   map[string]Handler

	// UnixPeerAllow, when non-nil, rejects /api requests whose Unix peer UID
	// does not pass this check (HTTP 403). When nil, no peer filter is applied
	// at the HTTP layer (DB-05).
	UnixPeerAllow func(uid int) bool

	// MaxAPIBodyBytes caps the raw JSON body for POST /api (default 4 MiB when 0).
	MaxAPIBodyBytes int

	// UnixAPIRatePerSec limits successful JSON decodes per peer UID per wall
	// clock second (default 200 when 0; set to -1 to disable, DB-06).
	UnixAPIRatePerSec int

	rateMu   sync.Mutex
	rateUnix map[int]*unixRateWindow
}

// NewServer creates an API server bound to the given socket path.
func NewServer(socketPath string, logger *zap.Logger) *Server {
	return &Server{
		socketPath: socketPath,
		logger:     logger,
		handlers:   make(map[string]Handler),
	}
}

// Handle registers a handler for the given action name.
func (s *Server) Handle(action string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[action] = h
}

// Start begins listening on the Unix socket. The parent directory is verified
// before binding so a privileged daemon never binds inside a user-controlled
// ~/.aegis socket directory on Linux.
// Phase 2 (04-unix-socket-hardening): Now uses enhanced SetRuntimeSocketOwner
// from paths.go (aegis group 0750 when available, abstract socket support via
// DefaultAbstractSocketPath(), SocketPermGroup). Enforces Task 1 hardened model.
func (s *Server) Start() error {
	if err := aegispaths.EnsureRuntimeDir(filepath.Dir(s.socketPath)); err != nil {
		return err
	}
	// Remove stale socket if it exists.
	os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	if err := aegispaths.SetRuntimeSocketOwner(s.socketPath); err != nil {
		ln.Close() //nolint:errcheck
		return err
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/api", s.handleAPI)

	srv := &http.Server{
		Handler: mux,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			uc, ok := c.(*net.UnixConn)
			if !ok {
				return ctx
			}
			raw, err := uc.SyscallConn()
			if err != nil {
				return ctx
			}
			uid, okUID := peerUIDFromRawConn(raw)
			if !okUID {
				return ctx
			}
			return context.WithValue(ctx, peerUIDContextKey{}, uid)
		},
	}
	go srv.Serve(ln)
	s.logger.Info("API server listening", zap.String("socket", s.socketPath))
	return nil
}

// Stop closes the listener and removes the socket file.
// CallDirect invokes a registered handler directly without going through the
// Unix socket. Used by the dashboard server (same process) to avoid a round trip.
func (s *Server) CallDirect(ctx context.Context, action string, data json.RawMessage) (resp *Response) {
	s.mu.RLock()
	h, ok := s.handlers[action]
	s.mu.RUnlock()
	if !ok {
		return &Response{Error: "unknown action: " + action}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if s.logger != nil {
				s.logger.Error("API handler panic",
					zap.String("action", action),
					zap.Any("panic", recovered),
					zap.ByteString("stack", debug.Stack()),
				)
			}
			resp = &Response{Error: "internal handler panic"}
	}
	}()
	return h(ctx, data)
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
}

// Closer returns a lightweight stop function that closes the listener and
// removes the socket file.  Unlike Stop, it captures only the net.Listener
// and the socket path string rather than the full Server value, so callers
// can discard the *Server pointer after calling Closer and let it be
// garbage-collected (e.g. from a goroutine that must not hold large closures).
func (s *Server) Closer() func() {
	if s == nil {
		return func() {}
	}
	ln := s.listener
	path := s.socketPath
	return func() {
		if ln != nil {
			ln.Close()
		}
		os.Remove(path)
	}
}

func (s *Server) maxBodyBytes() int64 {
	if s.MaxAPIBodyBytes > 0 {
		return int64(s.MaxAPIBodyBytes)
	}
	return int64(defaultMaxAPIBodyBytes)
}

func (s *Server) unixRateLimit() int {
	if s.UnixAPIRatePerSec == -1 {
		return -1
	}
	if s.UnixAPIRatePerSec > 0 {
		return s.UnixAPIRatePerSec
	}
	return 200
}

func (s *Server) allowUnixAPIRate(ctx context.Context) bool {
	limit := s.unixRateLimit()
	if limit < 0 {
		return true
	}
	uid, ok := PeerUIDFromContext(ctx)
	if !ok {
		uid = -1
	}
	now := time.Now().Unix()
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if s.rateUnix == nil {
		s.rateUnix = make(map[int]*unixRateWindow)
	}
	w := s.rateUnix[uid]
	if w == nil || w.second != now {
		s.rateUnix[uid] = &unixRateWindow{second: now, n: 1}
		return true
	}
	w.n++
	return w.n <= limit
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, &Response{Error: "POST required"})
		return
	}

	if s.UnixPeerAllow != nil {
		uid, ok := PeerUIDFromContext(r.Context())
		if !ok || !s.UnixPeerAllow(uid) {
			writeJSON(w, http.StatusForbidden, &Response{Error: "unix socket peer not authorized"})
			return
	}
	}

	max := s.maxBodyBytes()
	raw, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &Response{Error: "read body: " + err.Error()})
		return
	}
	if int64(len(raw)) > max {
		writeJSON(w, http.StatusRequestEntityTooLarge, &Response{Error: "request body too large"})
		return
	}

	var req Request
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, &Response{Error: "invalid JSON: " + err.Error()})
			return
	}
	}

	if !s.allowUnixAPIRate(r.Context()) {
		writeJSON(w, http.StatusTooManyRequests, &Response{Error: "unix api rate limit exceeded"})
		return
	}

	s.mu.RLock()
	h, ok := s.handlers[req.Action]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, &Response{Error: "unknown action: " + req.Action})
		return
	}

	resp := h(r.Context(), req.Data)
	status := http.StatusOK
	if !resp.Success {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
