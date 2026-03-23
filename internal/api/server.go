package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
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
	ProposalID string `json:"proposal_id"`
}

// DefaultSocketPath returns the default daemon socket path.
func DefaultSocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir + "/aegisclaw.sock"
	}
	return "/tmp/aegisclaw.sock"
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
	// Allow group access (like Docker's socket).
	os.Chmod(s.socketPath, 0660)
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/api", s.handleAPI)

	go http.Serve(ln, mux)
	s.logger.Info("API server listening", zap.String("socket", s.socketPath))
	return nil
}

// Stop closes the listener and removes the socket file.
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
