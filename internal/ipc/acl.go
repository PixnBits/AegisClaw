package ipc

import (
	"fmt"
	"sync"
)

// VMRole represents the trusted role assigned to a VM by the daemon at
// registration time. Roles are set once (at VM start) and are immutable.
type VMRole string

const (
	// RoleAgent is the main agent VM that drives the ReAct loop.
	RoleAgent VMRole = "agent"
	// RoleCLI represents the host CLI process connecting over the Unix API socket.
	RoleCLI VMRole = "cli"
	// RoleCourt represents a court reviewer VM.
	RoleCourt VMRole = "court"
	// RoleBuilder represents a builder pipeline VM.
	RoleBuilder VMRole = "builder"
	// RoleSkill represents a deployed skill VM.
	RoleSkill VMRole = "skill"
	// RoleHub is the AegisHub system microVM — the sole IPC router for the entire
	// system. It runs in its own Firecracker VM and is the only component that may
	// send routing-control messages to all other VMs. The host daemon registers
	// AegisHub with this role at startup, before any other VM is launched.
	RoleHub VMRole = "hub"
	// RoleDaemon represents the host daemon's tool-handler endpoint registered
	// with AegisHub. The daemon registers itself so that tool.result messages
	// from agent VMs (routed through AegisHub) can be delivered to it. The daemon
	// may send tool.result responses and status messages only — it does not
	// initiate routing calls (those go through the daemon's direct vsock API).
	RoleDaemon VMRole = "daemon"

	// RoleOrchestrator represents the optional Orchestrator microVM that injects
	// scheduled or event-driven chat.message payloads into AegisHub (PRD §10.6
	// A3 / architecture.md §8.2 A3).  It may only send event.trigger messages;
	// it cannot execute tools or modify proposals directly.
	// NOTE: This role is a roadmap placeholder.  The Orchestrator VM is not yet
	// launched by the daemon.  When implemented, the daemon will register it at
	// startup with this role using the same pattern as AegisHub.
	RoleOrchestrator VMRole = "orchestrator"

	// RolePlanner represents the optional Planner microVM that decomposes
	// high-level user goals into ordered sub-proposals (PRD §10.6 A5 /
	// architecture.md §8.2 A5).  It may only submit proposal.create_draft and
	// proposal.list_drafts tool calls; it cannot execute skills or initiate
	// high-risk actions.
	// NOTE: This role is a roadmap placeholder.  The Planner VM is not yet
	// launched by the daemon.
	RolePlanner VMRole = "planner"
)

// aclEntry is a single permit row: (role, messageType) → allowed.
// An empty messageType acts as a wildcard.
type aclEntry struct {
	role    VMRole
	msgType string // empty = any
}

// ACLPolicy holds the compiled-in default permit table.
// All traffic not on the allow list is denied.
type ACLPolicy struct {
	allowed map[aclEntry]struct{}
}

// defaultACLPolicy returns the security policy from architecture.md §5.1.
// The table is intentionally conservative; add rows when new message types
// are introduced and audited.
func defaultACLPolicy() *ACLPolicy {
	p := &ACLPolicy{allowed: make(map[aclEntry]struct{})}

	// Agent VM may send tool.exec, chat.* and status messages.
	p.permit(RoleAgent, "tool.exec")
	p.permit(RoleAgent, "chat.message")
	p.permit(RoleAgent, "status")

	// CLI process (host-side) may call any API action.
	p.permit(RoleCLI, "")

	// Court VMs may send review results back.
	p.permit(RoleCourt, "review.result")
	p.permit(RoleCourt, "status")

	// Builder VMs may report build results.
	p.permit(RoleBuilder, "build.result")
	p.permit(RoleBuilder, "status")

	// Skill VMs may only report tool results; they cannot initiate other calls.
	p.permit(RoleSkill, "tool.result")
	p.permit(RoleSkill, "status")

	// AegisHub (system router) may send any message type as part of its routing
	// and orchestration role. The daemon assigns this role to the AegisHub VM
	// at startup and never to any other VM.
	p.permit(RoleHub, "")

	// Daemon tool-handler endpoint: may receive tool.exec requests routed from
	// agent VMs via AegisHub, and may respond with tool.result. This entry
	// enables AegisHub to ACL-gate tool invocations — only registered
	// RoleDaemon endpoints receive tool.exec deliveries.
	p.permit(RoleDaemon, "tool.result")
	p.permit(RoleDaemon, "status")

	// Orchestrator microVM (roadmap — PRD §10.6 A3 / architecture.md §8.2 A3):
	// may only inject event.trigger messages into AegisHub.  AegisHub forwards
	// these as chat.message events to the agent VM after verifying the sender
	// role.  The Orchestrator cannot execute tools or reach the daemon directly.
	p.permit(RoleOrchestrator, "event.trigger")
	p.permit(RoleOrchestrator, "status")

	// Planner microVM (roadmap — PRD §10.6 A5 / architecture.md §8.2 A5):
	// may only create proposal drafts and list existing proposals.  It cannot
	// execute skills, submit proposals, or vote — those actions require explicit
	// human or agent approval.
	p.permit(RolePlanner, "tool.exec") // restricted to proposal.create_draft / list_drafts by convention
	p.permit(RolePlanner, "status")

	return p
}

func (p *ACLPolicy) permit(role VMRole, msgType string) {
	p.allowed[aclEntry{role: role, msgType: msgType}] = struct{}{}
}

// Check returns nil when the role is permitted to send msgType, otherwise an error.
func (p *ACLPolicy) Check(role VMRole, msgType string) error {
	// Wildcard: permit the role for all message types.
	if _, ok := p.allowed[aclEntry{role: role, msgType: ""}]; ok {
		return nil
	}
	// Exact match.
	if _, ok := p.allowed[aclEntry{role: role, msgType: msgType}]; ok {
		return nil
	}
	return fmt.Errorf("ACL denied: role %q is not permitted to send %q", role, msgType)
}

// IdentityRegistry maps VM IDs to their assigned VMRole.
// It is populated when the daemon starts a VM and cleaned up when it stops.
type IdentityRegistry struct {
	mu    sync.RWMutex
	roles map[string]VMRole // vmID → role
}

// NewIdentityRegistry creates an empty registry.
func NewIdentityRegistry() *IdentityRegistry {
	return &IdentityRegistry{roles: make(map[string]VMRole)}
}

// Register assigns a role to a VM ID. If the ID was already registered, this
// is a no-op and an error is returned to prevent accidental role escalation.
func (r *IdentityRegistry) Register(vmID string, role VMRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.roles[vmID]; ok {
		if existing == role {
			return nil // idempotent re-registration
		}
		return fmt.Errorf("VM %q already registered with role %q; cannot change to %q", vmID, existing, role)
	}
	r.roles[vmID] = role
	return nil
}

// Unregister removes a VM's identity on shutdown.
func (r *IdentityRegistry) Unregister(vmID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.roles, vmID)
}

// Role looks up the role for the given VM ID.
// Returns ("", false) when not registered.
func (r *IdentityRegistry) Role(vmID string) (VMRole, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role, ok := r.roles[vmID]
	return role, ok
}
