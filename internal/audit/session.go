package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SessionEventType identifies the kind of event recorded in a chat session log.
type SessionEventType string

const (
	EventSessionStart     SessionEventType = "session_start"
	EventSessionEnd       SessionEventType = "session_end"
	EventUserMessage      SessionEventType = "user_message"
	EventAssistantMessage SessionEventType = "assistant_message"
	EventToolCall         SessionEventType = "tool_call"
	EventToolResult       SessionEventType = "tool_result"
	EventSlashCommand     SessionEventType = "slash_command"
	EventSystemMessage    SessionEventType = "system_message"
)

// SessionEvent is a single entry in a chat session audit log.
type SessionEvent struct {
	Timestamp time.Time        `json:"timestamp"`
	SessionID string           `json:"session_id"`
	Event     SessionEventType `json:"event"`
	UserID    string           `json:"user_id,omitempty"` // populated when authN is available
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	ToolArgs  string           `json:"tool_args,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// SessionLog writes chat session events to a per-session JSONL file.
type SessionLog struct {
	sessionID string
	file      *os.File
	mu        sync.Mutex
}

// NewSessionLog creates a new session log file in the given directory.
// The file is named with the current timestamp and a UUID prefix for uniqueness.
func NewSessionLog(dir string) (*SessionLog, error) {
	sessDir := filepath.Join(dir, "chat-sessions")
	if err := os.MkdirAll(sessDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create chat-sessions directory: %w", err)
	}

	sessionID := uuid.New().String()
	ts := time.Now().UTC()
	filename := fmt.Sprintf("%s_%s.jsonl", ts.Format("2006-01-02T15-04-05"), sessionID[:8])
	path := filepath.Join(sessDir, filename)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create session log %s: %w", path, err)
	}

	sl := &SessionLog{
		sessionID: sessionID,
		file:      file,
	}

	// Write the opening session_start event.
	sl.Log(SessionEvent{
		Event: EventSessionStart,
	})

	return sl, nil
}

// SessionID returns the unique identifier for this session.
func (sl *SessionLog) SessionID() string {
	return sl.sessionID
}

// Log writes a session event to the log file. It fills in Timestamp and SessionID automatically.
func (sl *SessionLog) Log(evt SessionEvent) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	evt.Timestamp = time.Now().UTC()
	evt.SessionID = sl.sessionID

	line, err := json.Marshal(evt)
	if err != nil {
		// Best-effort: don't crash the chat if audit logging fails.
		return
	}
	line = append(line, '\n')
	sl.file.Write(line)
	sl.file.Sync()
}

// Close writes a session_end event and closes the file.
func (sl *SessionLog) Close() error {
	sl.Log(SessionEvent{
		Event: EventSessionEnd,
	})

	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.file.Close()
}
