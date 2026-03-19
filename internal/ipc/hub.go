package ipc

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

const (
	// MessageHubID is the well-known ID for the message-hub.
	MessageHubID = "message-hub"
)

// HubState represents the message-hub lifecycle state.
type HubState string

const (
	HubStateStarting HubState = "starting"
	HubStateRunning  HubState = "running"
	HubStateStopped  HubState = "stopped"
)

// HubStats tracks message-hub operational metrics.
type HubStats struct {
	MessagesRouted   uint64    `json:"messages_routed"`
	MessagesRejected uint64    `json:"messages_rejected"`
	DeliveryErrors   uint64    `json:"delivery_errors"`
	StartedAt        time.Time `json:"started_at"`
}

// MessageHub is the core IPC router skill. It runs in its own context
// (will be a microVM in production) and routes all inter-skill messages.
// No direct skill-to-skill communication is permitted.
type MessageHub struct {
	router *Router
	kern   *kernel.Kernel
	logger *zap.Logger
	state  HubState
	stats  HubStats
	mu     sync.RWMutex
}

// NewMessageHub creates the message-hub with the given kernel and logger.
func NewMessageHub(kern *kernel.Kernel, logger *zap.Logger) *MessageHub {
	return &MessageHub{
		router: NewRouter(),
		kern:   kern,
		logger: logger,
		state:  HubStateStopped,
	}
}

// Start initializes the message-hub and registers its own route handler.
func (h *MessageHub) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.state == HubStateRunning {
		return fmt.Errorf("message-hub is already running")
	}

	h.state = HubStateStarting

	// Register the hub's own handler for control messages
	if err := h.router.Register(MessageHubID, h.handleHubMessage); err != nil {
		h.state = HubStateStopped
		return fmt.Errorf("failed to register hub handler: %w", err)
	}

	h.stats = HubStats{
		StartedAt: time.Now().UTC(),
	}
	h.state = HubStateRunning

	h.logger.Info("message-hub started",
		zap.String("id", MessageHubID),
		zap.Time("started_at", h.stats.StartedAt),
	)

	return nil
}

// Stop shuts down the message-hub.
func (h *MessageHub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.router.Unregister(MessageHubID)
	h.state = HubStateStopped

	h.logger.Info("message-hub stopped",
		zap.Uint64("messages_routed", h.stats.MessagesRouted),
		zap.Uint64("messages_rejected", h.stats.MessagesRejected),
	)
}

// RegisterSkill registers a skill's message handler with the hub's router.
func (h *MessageHub) RegisterSkill(skillID string, handler RouteHandler) error {
	if skillID == MessageHubID {
		return fmt.Errorf("cannot register with reserved ID %q", MessageHubID)
	}

	if err := h.router.Register(skillID, handler); err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]string{
		"skill_id": skillID,
		"action":   "register",
	})
	action := kernel.NewAction(kernel.ActionSkillRegister, MessageHubID, payload)
	if _, err := h.kern.SignAndLog(action); err != nil {
		h.logger.Error("failed to log skill registration", zap.Error(err))
	}

	h.logger.Info("skill registered with message-hub",
		zap.String("skill_id", skillID),
	)
	return nil
}

// UnregisterSkill removes a skill's message handler from the router.
func (h *MessageHub) UnregisterSkill(skillID string) {
	h.router.Unregister(skillID)
	h.logger.Info("skill unregistered from message-hub",
		zap.String("skill_id", skillID),
	)
}

// RouteMessage validates, audits, and delivers a message from one skill to another.
// senderVMID is the verified identity from the vsock connection (not from the message).
func (h *MessageHub) RouteMessage(senderVMID string, msg *Message) (*DeliveryResult, error) {
	h.mu.RLock()
	if h.state != HubStateRunning {
		h.mu.RUnlock()
		return nil, fmt.Errorf("message-hub is not running")
	}
	h.mu.RUnlock()

	// Sign and audit the routing action
	payload, _ := json.Marshal(map[string]interface{}{
		"message_id":  msg.ID,
		"from":        msg.From,
		"to":          msg.To,
		"type":        msg.Type,
		"sender_vmid": senderVMID,
	})
	action := kernel.NewAction(kernel.ActionMessageRoute, MessageHubID, payload)
	if _, err := h.kern.SignAndLog(action); err != nil {
		h.logger.Error("failed to audit message routing",
			zap.String("message_id", msg.ID),
			zap.Error(err),
		)
	}

	result, err := h.router.Route(senderVMID, msg)
	if err != nil {
		h.mu.Lock()
		h.stats.MessagesRejected++
		h.mu.Unlock()

		h.logger.Warn("message rejected",
			zap.String("message_id", msg.ID),
			zap.String("from", msg.From),
			zap.String("to", msg.To),
			zap.Error(err),
		)
		return nil, err
	}

	h.mu.Lock()
	if result.Success {
		h.stats.MessagesRouted++
	} else {
		h.stats.DeliveryErrors++
	}
	h.mu.Unlock()

	h.logger.Info("message routed",
		zap.String("message_id", msg.ID),
		zap.String("from", msg.From),
		zap.String("to", msg.To),
		zap.Bool("success", result.Success),
	)

	return result, nil
}

// State returns the current hub state.
func (h *MessageHub) State() HubState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

// Stats returns a copy of the current hub stats.
func (h *MessageHub) Stats() HubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// Router returns the underlying IPC router.
func (h *MessageHub) Router() *Router {
	return h.router
}

// handleHubMessage processes control messages addressed to the hub itself.
func (h *MessageHub) handleHubMessage(msg *Message) (*DeliveryResult, error) {
	switch msg.Type {
	case "hub.status":
		stats := h.Stats()
		data, _ := json.Marshal(map[string]interface{}{
			"state":             h.State(),
			"messages_routed":   stats.MessagesRouted,
			"messages_rejected": stats.MessagesRejected,
			"delivery_errors":   stats.DeliveryErrors,
			"started_at":        stats.StartedAt,
			"routes":            h.router.RegisteredRoutes(),
		})
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   true,
			Response:  data,
		}, nil

	case "hub.routes":
		data, _ := json.Marshal(h.router.RegisteredRoutes())
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   true,
			Response:  data,
		}, nil

	default:
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     fmt.Sprintf("unknown hub command: %s", msg.Type),
		}, nil
	}
}
