// Package sessions implements the in-memory session registry for AegisClaw.
//
// A "session" in AegisClaw corresponds to a single ongoing conversation
// between a user and the agent.  The registry is used by the session routing
// tools (sessions_list, sessions_history, sessions_send, sessions_spawn)
// introduced in Phase 1 of the OpenClaw integration plan.
//
// Design decisions:
//   - In-memory only — sessions are ephemeral; history is not persisted across
//     daemon restarts.  Long-term memory is handled by the separate memory
//     vault (internal/memory).
//   - Thread-safe — all exported methods are safe to call from multiple
//     goroutines (the daemon's API handlers run concurrently).
//   - Bounded — message history is capped at maxMessagesPerSession to prevent
//     unbounded memory growth.  Oldest messages are dropped when the cap is
//     exceeded.  The session count is also bounded.
package sessions

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// maxMessagesPerSession is the maximum number of messages stored per
	// session.  When the cap is reached the oldest message is evicted.
	maxMessagesPerSession = 200

	// maxSessions is the upper bound on concurrent tracked sessions.
	// When reached, the oldest idle session is evicted before creating a new one.
	maxSessions = 100
)

// Status describes the current activity state of a session.
type Status string

const (
	// StatusActive means the session is actively processing a request.
	StatusActive Status = "active"

	// StatusIdle means the session is open but not currently processing.
	StatusIdle Status = "idle"

	// StatusClosed means the session has been closed / its VM stopped.
	StatusClosed Status = "closed"
)

// Message is a single conversation turn stored in a session.
type Message struct {
	// Role is "user", "assistant", or "tool".
	Role string `json:"role"`
	// Content is the text of the message.
	Content string `json:"content"`
	// Timestamp is when the message was appended.
	Timestamp time.Time `json:"timestamp"`
}

// Record holds metadata and history for one session.
type Record struct {
	// ID is the unique session identifier.
	ID string `json:"id"`

	// SandboxID is the ID of the agent microVM associated with this session.
	// Empty for sessions that have not yet launched a VM.
	SandboxID string `json:"sandbox_id,omitempty"`

	// StartedAt is the wall-clock time when the session was first opened.
	StartedAt time.Time `json:"started_at"`

	// LastActiveAt is the wall-clock time of the last message in this session.
	LastActiveAt time.Time `json:"last_active_at"`

	// Status is the current session status.
	Status Status `json:"status"`

	// messages are the capped rolling history of conversation turns.
	messages []Message
}

// Messages returns a copy of the session's message history.
// The most recent messages are at the end of the slice.
func (r *Record) Messages() []Message {
	if len(r.messages) == 0 {
		return nil
	}
	out := make([]Message, len(r.messages))
	copy(out, r.messages)
	return out
}

// Store is a thread-safe in-memory registry of chat sessions.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Record
	order    []string // insertion order; used for eviction
}

// NewStore returns an initialised, empty Store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*Record),
	}
}

// Open creates a new session record with the given id and sandbox VM ID.
// If a session with that id already exists its SandboxID is updated and
// the existing record is returned.  The session is placed in StatusIdle.
//
// When a previously closed session is re-opened (e.g. after the agent VM
// restarts), its status is reset to StatusIdle so that new messages can be
// appended and the session appears in listings again.
func (s *Store) Open(id, sandboxID string) *Record {
	if id == "" {
		id = uuid.New().String()
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if r, ok := s.sessions[id]; ok {
		if sandboxID != "" {
			r.SandboxID = sandboxID
		}
		// Re-activating a closed session is intentional: it allows the same
		// session_id to be reused after a VM restart without losing history.
		if r.Status == StatusClosed {
			r.Status = StatusIdle
		}
		r.LastActiveAt = now
		return r
	}

	// Evict the oldest idle session when at capacity.
	if len(s.sessions) >= maxSessions {
		s.evictOldestIdleLocked()
	}

	r := &Record{
		ID:           id,
		SandboxID:    sandboxID,
		StartedAt:    now,
		LastActiveAt: now,
		Status:       StatusIdle,
	}
	s.sessions[id] = r
	s.order = append(s.order, id)
	return r
}

// evictOldestIdleLocked removes the oldest idle (or closed) session.
// Must be called with s.mu held for writing.
func (s *Store) evictOldestIdleLocked() {
	for i, id := range s.order {
		r := s.sessions[id]
		if r != nil && (r.Status == StatusIdle || r.Status == StatusClosed) {
			delete(s.sessions, id)
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

// Get returns the record for id, or (nil, false) if not found.
func (s *Store) Get(id string) (*Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	// Return a shallow copy so callers cannot mutate the message slice
	// without going through AppendMessage.
	cp := *r
	return &cp, true
}

// List returns a snapshot of all sessions, sorted newest-last by LastActiveAt.
func (s *Store) List() []*Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Record, 0, len(s.sessions))
	for _, r := range s.sessions {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// AppendMessage adds a message to the session with the given id.
// If the session does not exist it is created in StatusIdle.
// If the session message buffer is full the oldest message is evicted.
func (s *Store) AppendMessage(id, sandboxID, role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.sessions[id]
	if !ok {
		if len(s.sessions) >= maxSessions {
			s.evictOldestIdleLocked()
		}
		now := time.Now().UTC()
		r = &Record{
			ID:           id,
			SandboxID:    sandboxID,
			StartedAt:    now,
			LastActiveAt: now,
			Status:       StatusIdle,
		}
		s.sessions[id] = r
		s.order = append(s.order, id)
	} else if sandboxID != "" && r.SandboxID == "" {
		r.SandboxID = sandboxID
	}

	msg := Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
	}
	r.messages = append(r.messages, msg)
	if len(r.messages) > maxMessagesPerSession {
		r.messages = r.messages[len(r.messages)-maxMessagesPerSession:]
	}
	r.LastActiveAt = time.Now().UTC()
}

// SetStatus updates the status of the session with the given id.
// If the session does not exist, SetStatus is a no-op.
func (s *Store) SetStatus(id string, status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.sessions[id]; ok {
		r.Status = status
	}
}

// Close marks the session as closed.  If the session does not exist it is
// a no-op.
func (s *Store) Close(id string) {
	s.SetStatus(id, StatusClosed)
}

// History returns up to limit messages from the session, most recent last.
// If limit <= 0 all stored messages are returned.
func (s *Store) History(id string, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}
	msgs := r.messages
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

// GenerateID returns a new unique session identifier.
func GenerateID() string {
	return uuid.New().String()
}
