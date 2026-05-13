package events

import (
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// ProposalStatusChangedEvent is emitted when a proposal's status changes.
// This enables event-driven reactions such as automatically triggering the builder
// pipeline when a proposal moves to "implementing" after Court approval.
type ProposalStatusChangedEvent struct {
	ProposalID string
	From       proposal.Status
	To         proposal.Status
	Reason     string
	Actor      string
	Timestamp  time.Time
	Proposal   *proposal.Proposal // snapshot at time of change (may be nil in some emitters)
}

// ProposalEventHandler is the function signature for handlers interested in proposal events.
type ProposalEventHandler func(event ProposalStatusChangedEvent)

// ProposalEventDispatcher provides a simple, thread-safe way to emit and subscribe to
// proposal lifecycle events. This is intentionally lightweight and domain-specific
// to keep long-term maintainability high (separate from the timer/approval eventbus).
type ProposalEventDispatcher struct {
	mu       sync.RWMutex
	handlers []ProposalEventHandler
}

// NewProposalEventDispatcher creates a new dispatcher.
func NewProposalEventDispatcher() *ProposalEventDispatcher {
	return &ProposalEventDispatcher{}
}

// Subscribe registers a handler. Handlers are called synchronously in registration order.
// For long-running work (e.g. builder pipeline), handlers should launch goroutines.
func (d *ProposalEventDispatcher) Subscribe(handler ProposalEventHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, handler)
}

// Emit sends the event to all registered handlers.
func (d *ProposalEventDispatcher) Emit(event ProposalStatusChangedEvent) {
	d.mu.RLock()
	handlers := make([]ProposalEventHandler, len(d.handlers))
	copy(handlers, d.handlers)
	d.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

// EmitStatusChanged is a convenience helper.
func (d *ProposalEventDispatcher) EmitStatusChanged(prop *proposal.Proposal, from, to proposal.Status, reason, actor string) {
	event := ProposalStatusChangedEvent{
		ProposalID: prop.ID,
		From:       from,
		To:         to,
		Reason:     reason,
		Actor:      actor,
		Timestamp:  time.Now().UTC(),
		Proposal:   prop,
	}
	d.Emit(event)
}
