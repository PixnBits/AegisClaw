// Package progress records live agent-loop thought events for chat UI streaming.
package progress

import (
	"strings"
	"sync"
	"time"
)

// ThoughtEvent matches the dashboard chat thought-log contract.
type ThoughtEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	Phase     string `json:"phase"`
	Summary   string `json:"summary,omitempty"`
	Details   string `json:"details,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Model     string `json:"model,omitempty"`
}

// StreamState holds in-flight stream progress for chat.stream_progress polls.
type StreamState struct {
	StreamID  string `json:"stream_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Thinking  string `json:"thinking"`
	Content   string `json:"content"`
}

var (
	mu           sync.Mutex
	nextID       int64
	events       []ThoughtEvent
	streams      = map[string]*StreamState{}
	sessionTrace = map[string][]ThoughtEvent{}
)

const maxEvents = 500

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// BeginTurn resets stream state and emits the first progress event.
func BeginTurn(sessionID, streamID string) {
	mu.Lock()
	defer mu.Unlock()
	if streamID != "" {
		streams[streamID] = &StreamState{
			StreamID:  streamID,
			SessionID: sessionID,
			RequestID: time.Now().UTC().Format(time.RFC3339Nano),
			Thinking:  "Starting agent loop…",
		}
	}
	appendLocked(sessionID, ThoughtEvent{
		Phase:     "starting",
		Summary:   "Starting agent loop",
		Details:   "Loading memory context and preparing the six-step reasoning cycle.",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// StepStarted emits progress when a loop phase begins.
func StepStarted(sessionID, streamID, phase, summary string) {
	mu.Lock()
	defer mu.Unlock()
	if st := streams[streamID]; st != nil {
		st.Thinking = summary
	}
	appendLocked(sessionID, ThoughtEvent{
		Phase:     phase,
		Summary:   summary,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// StepFinished records completion of a loop phase with optional model output excerpt.
func StepFinished(sessionID, streamID, phase, summary, details string) {
	mu.Lock()
	defer mu.Unlock()
	if st := streams[streamID]; st != nil {
		st.Thinking = summary
		if details != "" {
			st.Content = truncate(details, 240)
		}
	}
	appendLocked(sessionID, ThoughtEvent{
		Phase:     phase,
		Summary:   summary,
		Details:   truncate(details, 800),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// SetStreamContent updates visible partial response text during streaming.
func SetStreamContent(streamID, content string) {
	mu.Lock()
	defer mu.Unlock()
	if st := streams[streamID]; st != nil {
		st.Content = truncate(content, 400)
	}
}

// FinishTurn marks the stream complete and returns the thinking trace for the session.
func FinishTurn(sessionID, streamID, finalContent string) []ThoughtEvent {
	mu.Lock()
	defer mu.Unlock()
	if st := streams[streamID]; st != nil {
		st.Content = truncate(finalContent, 400)
		st.Thinking = "Response ready"
	}
	appendLocked(sessionID, ThoughtEvent{
		Phase:     "final",
		Summary:   "Response ready",
		Details:   truncate(finalContent, 800),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	return copyTrace(sessionID)
}

func appendLocked(sessionID string, ev ThoughtEvent) {
	nextID++
	ev.ID = nextID
	ev.SessionID = sessionID
	events = append(events, ev)
	if len(events) > maxEvents {
		events = events[len(events)-maxEvents:]
	}
	if sessionID != "" {
		sessionTrace[sessionID] = append(sessionTrace[sessionID], ev)
		if len(sessionTrace[sessionID]) > 80 {
			sessionTrace[sessionID] = sessionTrace[sessionID][len(sessionTrace[sessionID])-80:]
		}
	}
}

func copyTrace(sessionID string) []ThoughtEvent {
	src := sessionTrace[sessionID]
	out := make([]ThoughtEvent, len(src))
	copy(out, src)
	return out
}

// ListThoughtEvents returns recent thought events, optionally filtered by session.
func ListThoughtEvents(sessionID string, limit int) []ThoughtEvent {
	mu.Lock()
	defer mu.Unlock()
	if limit <= 0 {
		limit = 80
	}
	out := make([]ThoughtEvent, 0, limit)
	for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
		ev := events[i]
		if sessionID != "" && ev.SessionID != sessionID {
			continue
		}
		out = append(out, ev)
	}
	// reverse to chronological order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// StreamProgress returns current in-flight stream state.
func StreamProgress(streamID string) StreamState {
	mu.Lock()
	defer mu.Unlock()
	if st, ok := streams[streamID]; ok && st != nil {
		return *st
	}
	return StreamState{StreamID: streamID}
}

// TraceForSession returns the stored trace excluding the terminal marker for persistence.
func TraceForSession(sessionID string) []ThoughtEvent {
	mu.Lock()
	defer mu.Unlock()
	src := sessionTrace[sessionID]
	out := make([]ThoughtEvent, 0, len(src))
	for _, ev := range src {
		if ev.Phase == "final" {
			continue
		}
		out = append(out, ev)
	}
	return out
}
