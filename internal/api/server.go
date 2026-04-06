package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sync"

	"go.uber.org/zap"
)

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
	Args  string `json:"args,omitempty"`
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

// DefaultSocketPath returns the default daemon socket path.
// Uses a fixed, well-known location so the root daemon and unprivileged CLI
// always agree — similar to Docker's /var/run/docker.sock.
func DefaultSocketPath() string {
	return "/run/aegisclaw.sock"
}

// Server listens on a Unix socket and dispatches incoming requests to
// registered handlers keyed by action name.
type Server struct {
	socketPath string
	logger     *zap.Logger
	listener   net.Listener
	mu         sync.RWMutex
	handlers   map[string]Handler
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

// Start begins listening on the Unix socket. It removes any stale socket
// file and sets permissions so that group members can connect.
func (s *Server) Start() error {
	// Remove stale socket if it exists.
	os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	// Allow any local user to connect (like Docker's default socket).
	os.Chmod(s.socketPath, 0666)
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/api", s.handleAPI)

	go http.Serve(ln, mux)
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

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, &Response{Error: "POST required"})
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &Response{Error: "invalid JSON: " + err.Error()})
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
