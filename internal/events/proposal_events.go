package events

import (
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// ProposalStatusChangedEvent is emitted when a proposal's status changes.
type ProposalStatusChangedEvent struct {
	ProposalID string
	From       proposal.Status
	To         proposal.Status
	Reason     string
	Actor      string
	Timestamp  time.Time
	Proposal   *proposal.Proposal
}

type ProposalEventHandler func(event ProposalStatusChangedEvent)

type ProposalEventDispatcher struct {
	mu       sync.RWMutex
	handlers []ProposalEventHandler
}

func NewProposalEventDispatcher() *ProposalEventDispatcher {
	return &ProposalEventDispatcher{}
}

func (d *ProposalEventDispatcher) Subscribe(handler ProposalEventHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, handler)
}

func (d *ProposalEventDispatcher) Emit(event ProposalStatusChangedEvent) {
	d.mu.RLock()
	handlers := make([]ProposalEventHandler, len(d.handlers))
	copy(handlers, d.handlers)
	d.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

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
