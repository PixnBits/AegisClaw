package ipc

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Message is the canonical IPC envelope routed between sandboxes via the kernel.
type Message struct {
	ID        string          `json:"id"`
	From      string          `json:"from"`
	To        string          `json:"to"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Validate ensures the message has required fields for routing.
func (m *Message) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("message ID is required")
	}
	if m.From == "" {
		return fmt.Errorf("sender (from) is required")
	}
	if m.To == "" {
		return fmt.Errorf("recipient (to) is required")
	}
	if m.Type == "" {
		return fmt.Errorf("message type is required")
	}
	return nil
}

// DeliveryResult indicates the outcome of a message delivery attempt.
type DeliveryResult struct {
	MessageID string          `json:"message_id"`
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	Response  json.RawMessage `json:"response,omitempty"`
}

// RouteHandler processes a message delivered to a VM and returns a response.
type RouteHandler func(msg *Message) (*DeliveryResult, error)

// Router mediates all inter-VM communication. No direct VM-to-VM paths exist.
// Every message is validated for sender identity and routed through the kernel.
type Router struct {
	handlers map[string]RouteHandler // keyed by VM/skill ID
	mu       sync.RWMutex
}

// NewRouter creates a new IPC router.
func NewRouter() *Router {
	return &Router{
		handlers: make(map[string]RouteHandler),
	}
}

// Register adds a route handler for a VM or skill ID. The router will deliver
// messages addressed to this ID via the handler.
func (r *Router) Register(id string, handler RouteHandler) error {
	if id == "" {
		return fmt.Errorf("route ID must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("handler must not be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[id]; exists {
		return fmt.Errorf("route already registered for %q", id)
	}

	r.handlers[id] = handler
	return nil
}

// Unregister removes a route handler for the given ID.
func (r *Router) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, id)
}

// Route validates sender identity and delivers a message to the target.
// The senderVMID parameter is the verified VM identity from the vsock connection,
// not from the message itself (prevents spoofing).
func (r *Router) Route(senderVMID string, msg *Message) (*DeliveryResult, error) {
	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid message: %w", err)
	}

	// Sender identity verification: the From field must match the authenticated
	// VM identity from the vsock connection. This prevents VMs from spoofing
	// messages as other VMs.
	if msg.From != senderVMID {
		return nil, fmt.Errorf(
			"sender identity mismatch: message claims from=%q but vsock identity is %q",
			msg.From, senderVMID,
		)
	}

	// No self-routing
	if msg.From == msg.To {
		return nil, fmt.Errorf("self-routing is not permitted (from == to == %q)", msg.From)
	}

	r.mu.RLock()
	handler, exists := r.handlers[msg.To]
	r.mu.RUnlock()

	if !exists {
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     fmt.Sprintf("no route to %q", msg.To),
		}, nil
	}

	result, err := handler(msg)
	if err != nil {
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     fmt.Sprintf("delivery failed: %v", err),
		}, nil
	}

	return result, nil
}

// RegisteredRoutes returns the list of all registered route IDs.
func (r *Router) RegisteredRoutes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]string, 0, len(r.handlers))
	for id := range r.handlers {
		routes = append(routes, id)
	}
	return routes
}

// HasRoute checks if a route exists for the given ID.
func (r *Router) HasRoute(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.handlers[id]
	return exists
}
