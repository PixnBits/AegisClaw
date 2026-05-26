// Package eventbus provides a lightweight, in-process event bus for AegisClaw.
// 
// Design notes (per docs/specs/event-system.md + additional-requirements-and-gaps.md):
// - This is the *internal* (in-process) bus for fast coordination inside a single
//   Go binary (orchestrator, web-portal thin layer, etc.).
// - Important cross-component / cross-VM events are still routed through AegisHub
//   (signed + audited) as the authoritative mediator.
// - Events are named (e.g. "court.decision.made", "timer.fired", "autonomy.granted").
// - Payloads are JSON for easy serialization across boundaries.
// - Supports fire-and-forget + simple request/response patterns via correlation.
// - Timer / scheduled background task support is included (one-shot for now).
//
// Security / TCB considerations:
// - No secrets in events.
// - Subscribers are responsible for authorization checks on sensitive events.
// - Use bounded channels or non-blocking sends to avoid back-pressure attacks.
// - Trace IDs for audit correlation.
//
// This is the foundation for Task 7.2 (EventBus & Background Services).
package eventbus

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Event represents a named occurrence in the system.
type Event struct {
	Name      string          `json:"name"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	TraceID   string          `json:"trace_id,omitempty"`
	Source    string          `json:"source,omitempty"` // e.g. "orchestrator", "cli", "hub"
}

// Handler is the signature for event subscribers.
type Handler func(Event)

// Subscription allows unsubscribing from an event.
type Subscription struct {
	name    string
	id      int
	unsub   func()
}

// Unsubscribe removes this handler from the bus.
func (s *Subscription) Unsubscribe() {
	if s.unsub != nil {
		s.unsub()
	}
}

// Bus is a simple, thread-safe in-process event bus.
type Bus struct {
	mu            sync.RWMutex
	subscribers   map[string]map[int]Handler
	timers        map[string]*time.Timer // active scheduled timers for cancellation
	nextID        int
	publishErrors atomic.Int64 // 7.2 observability: recovered panics in handler goroutines
}

// New creates a new Bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string]map[int]Handler),
		timers:      make(map[string]*time.Timer),
	}
}

// Publish sends an event to all current subscribers of that name.
// Delivery is best-effort and non-blocking for individual handlers.
func (b *Bus) Publish(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	b.mu.RLock()
	handlers, ok := b.subscribers[e.Name]
	if !ok || len(handlers) == 0 {
		b.mu.RUnlock()
		return
	}

	// Snapshot the handlers so we can release the lock quickly
	snapshot := make([]Handler, 0, len(handlers))
	for _, h := range handlers {
		snapshot = append(snapshot, h)
	}
	b.mu.RUnlock()

	for _, h := range snapshot {
		// Run handlers in goroutines to avoid blocking the publisher.
		// In a more advanced version we would have bounded workers or backpressure.
		go func(handler Handler, evt Event) {
			defer func() {
				// Never let a panicking handler kill the bus.
				// 7.2: count recovered panics for autonomous observability and debugging.
				if r := recover(); r != nil {
					b.publishErrors.Add(1)
				}
			}()
			handler(evt)
		}(h, e)
	}
}

// Subscribe registers a handler for events with the given name.
// Returns a Subscription that can be used to unsubscribe.
func (b *Bus) Subscribe(name string, handler Handler) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscribers[name] == nil {
		b.subscribers[name] = make(map[int]Handler)
	}

	id := b.nextID
	b.nextID++
	b.subscribers[name][id] = handler

	return &Subscription{
		name: name,
		id:   id,
		unsub: func() {
			b.mu.Lock()
			delete(b.subscribers[name], id)
			if len(b.subscribers[name]) == 0 {
				delete(b.subscribers, name)
			}
			b.mu.Unlock()
		},
	}
}

// PublishJSON is a convenience helper that marshals the payload for you.
func (b *Bus) PublishJSON(name string, payload interface{}, opts ...PublishOption) {
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err == nil {
			raw = data
		}
	}

	e := Event{
		Name:    name,
		Payload: raw,
	}
	for _, opt := range opts {
		opt(&e)
	}
	b.Publish(e)
}

// PublishOption customizes an event before publishing.
type PublishOption func(*Event)

func WithTraceID(traceID string) PublishOption {
	return func(e *Event) { e.TraceID = traceID }
}

func WithSource(source string) PublishOption {
	return func(e *Event) { e.Source = source }
}

// DefaultBus is a package-level bus for convenience in early wiring.
// Long-term components should accept a *Bus via dependency injection.
var DefaultBus = New()

// Publish and Subscribe on the default bus for very simple use cases.
func Publish(e Event) { DefaultBus.Publish(e) }
func Subscribe(name string, h Handler) *Subscription { return DefaultBus.Subscribe(name, h) }
func PublishJSON(name string, payload interface{}, opts ...PublishOption) {
	DefaultBus.PublishJSON(name, payload, opts...)
}

// --- Timer / Scheduled Background Task Support (Task 7.2) ---
//
// Timers fire by publishing a "timer.fired" (or caller-specified) event.
// This directly supports autonomy durations, team task scheduling, and
// background services from the user journeys.
//
// Persistence (Store VM) and recurring timers are future work.

type Timer struct {
	ID        string          `json:"id"`
	EventName string          `json:"event_name"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// ScheduleTimer schedules a one-shot timer. When it fires it publishes an event
// (named eventName, or "timer.fired" if empty) containing the original payload
// plus timer metadata.
func (b *Bus) ScheduleTimer(d time.Duration, eventName string, payload any, opts ...PublishOption) string {
	if eventName == "" {
		eventName = "timer.fired"
	}

	var raw json.RawMessage
	if payload != nil {
		if data, err := json.Marshal(payload); err == nil {
			raw = data
		}
	}

	id := fmt.Sprintf("tmr-%d", time.Now().UnixNano())

	timerInfo := Timer{
		ID:        id,
		EventName: eventName,
		Payload:   raw,
		ExpiresAt: time.Now().Add(d).UTC(),
	}

	// We store the timer handle so we can cancel it.
	t := time.AfterFunc(d, func() {
		// Build the event to publish on fire
		firePayload := map[string]interface{}{
			"timer":     timerInfo,
			"original":  payload,
		}
		b.PublishJSON(eventName, firePayload, append(opts, WithSource("eventbus.timer"))...)
	})

	b.mu.Lock()
	b.timers[id] = t
	b.mu.Unlock()

	return id
}

// CancelTimer attempts to cancel a previously scheduled timer.
// Returns true if the timer was successfully cancelled before firing.
func (b *Bus) CancelTimer(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if t, ok := b.timers[id]; ok {
		stopped := t.Stop()
		delete(b.timers, id)
		return stopped
	}
	return false
}

// timers holds active time.Timer handles for cancellation (not exported).
// Note: We extend the Bus struct here via the methods above (the field is added lazily).
// For a production version we would initialize it in New().

// End of timer support.

// --- 7.2 Observability helpers (7.2.1.1) ---

// ErrorCount returns the number of panics that have been recovered inside
// handler goroutines. This is a lightweight, cheap observability signal for
// Task 7.2 background services and autonomous debugging. It never blocks.
func (b *Bus) ErrorCount() int64 {
	return b.publishErrors.Load()
}
