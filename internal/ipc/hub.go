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
	router   *Router
	kern     *kernel.Kernel
	logger   *zap.Logger
	state    HubState
	stats    HubStats
	mu       sync.RWMutex
	identity *IdentityRegistry
	acl      *ACLPolicy
}

// NewMessageHub creates the message-hub with the given kernel and logger.
func NewMessageHub(kern *kernel.Kernel, logger *zap.Logger) *MessageHub {
	return &MessageHub{
		router:   NewRouter(),
		kern:     kern,
		logger:   logger,
		state:    HubStateStopped,
		identity: NewIdentityRegistry(),
		acl:      defaultACLPolicy(),
	}
}

// NewMessageHubNoKernel creates a MessageHub without kernel audit logging.
// Use this when the hub runs inside a microVM (e.g. AegisHub) where the host
// kernel instance is not available. All routing and ACL logic is identical;
// only audit-log writes to the Merkle chain are skipped.
func NewMessageHubNoKernel(logger *zap.Logger) *MessageHub {
	return &MessageHub{
		router:   NewRouter(),
		kern:     nil, // intentional: running inside a microVM, no host kernel singleton available
		logger:   logger,
		state:    HubStateStopped,
		identity: NewIdentityRegistry(),
		acl:      defaultACLPolicy(),
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

	if h.kern != nil {
		payload, _ := json.Marshal(map[string]string{
			"skill_id": skillID,
			"action":   "register",
		})
		action := kernel.NewAction(kernel.ActionSkillRegister, MessageHubID, payload)
		if _, err := h.kern.SignAndLog(action); err != nil {
			h.logger.Error("failed to log skill registration", zap.Error(err))
		}
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

// RegisterVM associates a VM ID with its access-control role (DA).
// Must be called when a VM is started so that RouteMessage can enforce the ACL.
func (h *MessageHub) RegisterVM(vmID string, role VMRole) error {
	if err := h.identity.Register(vmID, role); err != nil {
		return err
	}
	h.logger.Info("VM registered with identity",
		zap.String("vm_id", vmID),
		zap.String("role", string(role)),
	)
	return nil
}

// UnregisterVM removes a VM's identity entry on shutdown.
func (h *MessageHub) UnregisterVM(vmID string) {
	h.identity.Unregister(vmID)
	h.logger.Info("VM identity unregistered", zap.String("vm_id", vmID))
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
	if h.kern != nil {
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
	}

	// ACL enforcement (DA): check that the sender's role is permitted to send
	// this message type before routing.
	senderRole, registered := h.identity.Role(senderVMID)
	if !registered {
		// Unknown sender — allow only if it looks like the CLI (empty vmID is
		// used by the bridge for host-originated messages).
		if senderVMID != "" {
			h.mu.Lock()
			h.stats.MessagesRejected++
			h.mu.Unlock()
			return nil, fmt.Errorf("IPC denied: VM %q has no registered identity", senderVMID)
		}
		senderRole = RoleCLI
	}
	if aclErr := h.acl.Check(senderRole, msg.Type); aclErr != nil {
		h.mu.Lock()
		h.stats.MessagesRejected++
		h.mu.Unlock()
		h.logger.Warn("ACL denied message",
			zap.String("vm_id", senderVMID),
			zap.String("role", string(senderRole)),
			zap.String("msg_type", msg.Type),
		)
		return nil, aclErr
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
