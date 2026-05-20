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
//
// Phase 6: MessageHub also serves as the entry point for ControlPlaneRequest
// messages coming from the Host Daemon's ControlPlaneProxy. CLI operations
// (worker.list, skill.status, chat.message, etc.) are forwarded here for
// ACL enforcement and routing to the correct target component.
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

// RegisterIdentityForTest pre-registers a VM ID with a role so that ACL checks
// in RouteMessage succeed for test scenarios. Exported solely to support
// cross-package test adaptation after TCB reduction (ControlPlaneProxy mediation).
func (h *MessageHub) RegisterIdentityForTest(vmID string, role VMRole) error {
	if h.identity == nil {
		return fmt.Errorf("identity registry not initialized")
	}
	return h.identity.Register(vmID, role)
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

	case "controlplane.request":
		return h.handleControlPlaneRequest(msg)

	default:
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     fmt.Sprintf("unknown hub command: %s", msg.Type),
		}, nil
	}
}

// handleControlPlaneRequest parses a ControlPlaneRequest from the message payload
// and dispatches to the appropriate backend based on the Action field.
// Phase 8: This is the AegisHub receiving side for mediated CLI operations.
func (h *MessageHub) handleControlPlaneRequest(msg *Message) (*DeliveryResult, error) {
	var req struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "invalid controlplane request payload",
		}, nil
	}

	h.logger.Debug("ControlPlaneRequest received",
		zap.String("action", req.Action),
		zap.String("from", msg.From))

	// Phase 8 improvement: attempt delegation to a registered backend first.
	// If a backend (e.g., "store-vm") is registered for the action, forward
	// the request to it via the RouteHandler. This enables realistic
	// end-to-end testing and future Store VM integration.
	// Error mapping: handler errors or nil results fall through to sample
	// fallback; we never surface Go errors from delegation here because
	// ControlPlaneProxy expects a DeliveryResult (with .Error set on failure).
	if backendID := h.preferredBackendForAction(req.Action); backendID != "" {
		if handler, ok := h.getRegisteredHandler(backendID); ok {
			delegatedMsg := &Message{
				ID:        msg.ID,
				From:      msg.From,
				To:        backendID,
				Type:      req.Action,
				Payload:   req.Data,
				Timestamp: time.Now().UTC(),
			}
			if result, err := handler(delegatedMsg); err == nil && result != nil {
				return result, nil
			}
			// Delegation attempt failed (handler error or nil result); log and
			// fall through to sample data for robustness / graceful degradation.
			if h.logger != nil {
				h.logger.Debug("ControlPlaneRequest delegation failed, using sample fallback",
					zap.String("action", req.Action), zap.String("backend", backendID))
			}
		}
	}

	// Phase 9: For proposal actions, never fall back to sample data.
	// If the "store-vm" backend (proposalBackend wrapping real ProposalStore) is not
	// registered at AegisHub startup, return a clear actionable error + log.
	// This enforces that real data is used in production and makes missing wiring obvious.
	if req.Action == "proposal.list" || req.Action == "proposal.status" {
		if h.logger != nil {
			h.logger.Warn("proposal action with no store-vm backend registered; returning error (no silent sample fallback)",
				zap.String("action", req.Action))
		}
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     "store-vm backend not registered (real ProposalStore required at AegisHub startup)",
		}, nil
	}

	// Fallback sample data for actions that have no registered backend yet.
	// This path is used when delegation fails or no backend is registered.
	// Real implementations will come from Store VM or other microVMs (Phase 9).
	if h.logger != nil {
		h.logger.Debug("ControlPlaneRequest using sample fallback",
			zap.String("action", req.Action))
	}
	switch req.Action {
	case "worker.list":
		data := json.RawMessage(`[{"worker_id":"w-001","role":"general","status":"idle"}]`)
		return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil

	case "worker.status":
		data := json.RawMessage(`{"worker_id":"w-001","role":"general","status":"idle","task_id":""}`)
		return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil

	case "skill.list":
		data := json.RawMessage(`[{"skill_id":"example-skill","status":"registered"}]`)
		return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil

	case "chat.message":
		// Delegated to chat router / agent VM in production via preferredBackendForAction + RegisterSkill.
		// Current fallback provides basic session awareness (echo + prev context simulation)
		// and structured response when no chat-router backend is registered.
		// A real chatRouter would maintain session history and route to Agent VMs.
		var in struct {
			Message     string `json:"message"`
			SessionID   string `json:"session_id"`
			Correlation string `json:"correlation_id"`
		}
		_ = json.Unmarshal(req.Data, &in)
		if in.SessionID == "" {
			in.SessionID = "s-001"
		}
		if in.Correlation == "" {
			in.Correlation = "corr-" + time.Now().Format("150405")
		}
		reply := map[string]interface{}{
			"session_id":     in.SessionID,
			"reply":          "echo: " + in.Message,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
			"correlation_id": in.Correlation,
		}
		data, _ := json.Marshal(reply)
		return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil

	// proposal.list / proposal.status no longer have sample fallback here.
	// They are handled exclusively by the registered proposalBackend (or error if missing).
	// See guard above + preferredBackendForAction.

	default:
		return &DeliveryResult{
			MessageID: msg.ID,
			Success:   false,
			Error:     fmt.Sprintf("unsupported controlplane action: %s", req.Action),
		}, nil
	}
}

// preferredBackendForAction returns the preferred backend skill/VM ID for a
// given ControlPlane action. This mapping makes delegation explicit and easy
// to extend when real backends (Store VM, etc.) are registered.
//
// How to Add a New Backend (e.g. "store-vm", "chat-router"):
//   1. Define an adapter type that holds your real backend (e.g. ProposalStore)
//      and implements a RouteHandler func(*Message) (*DeliveryResult, error).
//      Best practice: keep adapters stateless or inject deps; see proposalBackend
//      pattern for wrapping git-backed stores.
//   2. At daemon/AegisHub startup (or in Store VM init), call:
//        hub.RegisterSkill("store-vm", myAdapter.Handle)
//      This wires the handler into the router so getRegisteredHandler finds it.
//   3. preferredBackendForAction will route matching actions (e.g. proposal.list)
//      to it first; the registered handler wins over internal sample fallback.
//   The ControlPlaneProxy + handleControlPlaneRequest flow then delegates
//   transparently. Registering multiple backends is supported for different actions.
func (h *MessageHub) preferredBackendForAction(action string) string {
	switch action {
	case "worker.list", "worker.status":
		return "store-vm"
	case "skill.list":
		return "skill-registry"
	case "chat.message":
		return "chat-router"
	case "proposal.list", "proposal.status":
		return "store-vm"
	default:
		return ""
	}
}

// getRegisteredHandler looks up a registered route handler for delegation.
func (h *MessageHub) getRegisteredHandler(id string) (RouteHandler, bool) {
	if h.router == nil {
		return nil, false
	}
	return h.router.handlerFor(id)
}
