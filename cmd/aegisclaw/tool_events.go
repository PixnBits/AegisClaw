package main

import (
	"sync"
	"time"
)

// ToolCallEvent is an immutable event emitted during a ReAct tool invocation.
type ToolCallEvent struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Tool       string    `json:"tool"`
	Phase      string    `json:"phase"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

// ToolEventBuffer stores recent tool-call events for dashboard/API consumers.
type ToolEventBuffer struct {
	mu     sync.RWMutex
	nextID int64
	max    int
	events []ToolCallEvent
}

func NewToolEventBuffer(max int) *ToolEventBuffer {
	if max < 1 {
		max = 100
	}
	return &ToolEventBuffer{max: max}
}

func (b *ToolEventBuffer) appendEvent(ev ToolCallEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	ev.ID = b.nextID
	b.events = append(b.events, ev)
	if len(b.events) > b.max {
		b.events = b.events[len(b.events)-b.max:]
	}
}

func (b *ToolEventBuffer) RecordStart(tool string) {
	b.appendEvent(ToolCallEvent{
		Timestamp: time.Now().UTC(),
		Tool:      tool,
		Phase:     "start",
		Success:   true,
	})
}

func (b *ToolEventBuffer) RecordFinish(tool string, success bool, err error, duration time.Duration) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	b.appendEvent(ToolCallEvent{
		Timestamp:  time.Now().UTC(),
		Tool:       tool,
		Phase:      "finish",
		Success:    success,
		Error:      errMsg,
		DurationMS: duration.Milliseconds(),
	})
}

func (b *ToolEventBuffer) Recent(limit int) []ToolCallEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}
	if limit > len(b.events) {
		limit = len(b.events)
	}
	if limit == 0 {
		return []ToolCallEvent{}
	}
	out := make([]ToolCallEvent, limit)
	copy(out, b.events[len(b.events)-limit:])
	return out
}
