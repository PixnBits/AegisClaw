package main

import (
	"sync"
	"time"
)

// ThoughtEvent captures a lightweight model reasoning step for dashboard display.
type ThoughtEvent struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Phase     string    `json:"phase"`
	Tool      string    `json:"tool,omitempty"`
	Summary   string    `json:"summary"`
	Details   string    `json:"details,omitempty"`
}

// ThoughtEventBuffer stores recent thought events for dashboard/API consumers.
type ThoughtEventBuffer struct {
	mu     sync.RWMutex
	nextID int64
	max    int
	events []ThoughtEvent
}

func NewThoughtEventBuffer(max int) *ThoughtEventBuffer {
	if max < 1 {
		max = 200
	}
	return &ThoughtEventBuffer{max: max}
}

func (b *ThoughtEventBuffer) appendEvent(ev ThoughtEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	ev.ID = b.nextID
	b.events = append(b.events, ev)
	if len(b.events) > b.max {
		b.events = b.events[len(b.events)-b.max:]
	}
}

func (b *ThoughtEventBuffer) Record(phase, tool, summary, details string) {
	b.appendEvent(ThoughtEvent{
		Timestamp: time.Now().UTC(),
		Phase:     phase,
		Tool:      tool,
		Summary:   summary,
		Details:   details,
	})
}

func (b *ThoughtEventBuffer) Recent(limit int) []ThoughtEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 {
		limit = 30
	}
	if limit > len(b.events) {
		limit = len(b.events)
	}
	if limit == 0 {
		return []ThoughtEvent{}
	}
	out := make([]ThoughtEvent, limit)
	copy(out, b.events[len(b.events)-limit:])
	return out
}
