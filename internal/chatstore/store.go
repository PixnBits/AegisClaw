// Package chatstore persists web-portal chat session records in the Store VM
// (not in the browser or Host Daemon). The Web Portal forwards session CRUD to
// Store via AegisHub; chat turns flow through the agent chat system separately.
package chatstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Message is one turn in a chat session (user, assistant, or error).
type Message struct {
	Role          string          `json:"role"`
	Content       string          `json:"content"`
	Model         string          `json:"model,omitempty"`
	ToolCalls     json.RawMessage `json:"tool_calls,omitempty"`
	ThinkingTrace json.RawMessage `json:"thinking_trace,omitempty"`
}

// Session is a persisted conversation thread for the web-portal /chat UI.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt int64     `json:"created_at"`
	UpdatedAt int64     `json:"updated_at"`
	Messages  []Message `json:"messages,omitempty"`
}

// Summary is session metadata without message bodies (for sidebar lists).
type Summary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Store is a JSON file-backed chat session registry.
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a store at the given file path (typically ~/.aegis/chat-sessions.json).
func New(path string) *Store {
	return &Store{path: path}
}

func (s *Store) loadLocked() ([]Session, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, err
	}
	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parse chat sessions: %w", err)
	}
	if sessions == nil {
		sessions = []Session{}
	}
	return sessions, nil
}

func (s *Store) saveLocked(sessions []Session) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ListSummaries returns all sessions ordered by updated_at descending.
func (s *Store) ListSummaries() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, Summary{
			ID:        sess.ID,
			Title:     sess.Title,
			CreatedAt: sess.CreatedAt,
			UpdatedAt: sess.UpdatedAt,
		})
	}
	// Newest first (matches prior localStorage unshift behavior).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt > out[i].UpdatedAt {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// Get returns one session including messages.
func (s *Store) Get(id string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.loadLocked()
	if err != nil {
		return Session{}, false, err
	}
	for _, sess := range sessions {
		if sess.ID == id {
			return sess, true, nil
		}
	}
	return Session{}, false, nil
}

// Create adds a new empty session and returns it.
func (s *Store) Create(title string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.loadLocked()
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UnixMilli()
	if title == "" {
		title = "New session"
	}
	sess := Session{
		ID:        newID(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []Message{},
	}
	sessions = append([]Session{sess}, sessions...)
	if err := s.saveLocked(sessions); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Save updates title and/or messages for an existing session.
func (s *Store) Save(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions, err := s.loadLocked()
	if err != nil {
		return err
	}
	found := false
	sess.UpdatedAt = time.Now().UnixMilli()
	for i := range sessions {
		if sessions[i].ID == sess.ID {
			if sess.Title != "" {
				sessions[i].Title = sess.Title
			}
			if sess.Messages != nil {
				sessions[i].Messages = sess.Messages
			}
			sessions[i].UpdatedAt = sess.UpdatedAt
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("session %q not found", sess.ID)
	}
	return s.saveLocked(sessions)
}

func newID() string {
	return fmt.Sprintf("%x%x", time.Now().UnixNano(), time.Now().UnixNano()&0xffff)
}
