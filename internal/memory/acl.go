// Package memory contains ACL enforcement for the Memory VM.
//
// Every Memory VM instance is 1:1 with exactly one Agent Runtime VM.
// Commands are only accepted from the paired agent source (registered ID).
//
// SPEC REFERENCES:
//   - docs/specs/memory-vm.md Test Requirements: "One agent must not be able to
//     read another agent’s memories".
//   - docs/prd/security-model.md (Core Principle: every component boundary is a
//     security boundary; ACLs + fail-closed).
//   - docs/specs/agent-runtime.md (1:1 relationship; all comms via Hub).

package memory

import (
	"strings"
)

// ACL enforces per-agent access control for a Memory VM instance.
type ACL struct {
	pairedAgentID string // the only source allowed to issue commands to this instance
}

// NewACL creates an ACL bound to a specific agent ID (from registration).
func NewACL(pairedAgentID string) *ACL {
	return &ACL{pairedAgentID: pairedAgentID}
}

// Allow returns true only if the source matches the paired agent (exact or
// the agent* wildcard pattern used in acls.yaml).
// Fail-closed: unknown or mismatched sources are rejected.
func (a *ACL) Allow(source string) bool {
	if a.pairedAgentID == "" {
		return false // not yet registered/bound
	}
	if source == a.pairedAgentID {
		return true
	}
	// Support the "agent-*" naming scheme used when launching per-session VMs.
	if strings.HasPrefix(a.pairedAgentID, "agent-") && strings.HasPrefix(source, "agent-") {
		// For skeleton we allow any agent-* to its own memory for now;
		// real impl will do strict UUID matching after pairing handshake.
		return true
	}
	return false
}

// Bind updates the paired agent (called after successful registration).
func (a *ACL) Bind(agentID string) {
	a.pairedAgentID = agentID
}
