package contracts

import "strings"

// Bridge actions the portal may invoke via the vsock bridge (allow-list).
// High-impact actions require user confirmation in the UI before calling.

var allowedBridgeActions = map[string]struct{}{
	// Channels
	"channel.list":         {},
	"channel.get":          {},
	"channel.create":       {},
	"channel.post":         {},
	"channel.fanout":       {},
	"channel.archive":      {},
	"channel.add_member":   {},
	"channel.remove_member": {},

	// Skills & proposals
	"skill.list":           {},
	"proposal.list":        {},
	"proposal.get":         {},
	"proposal.create":      {},
	"proposal.get_audit":   {},

	// Court & governance
	"event.approvals.list": {},
	"court.get_reviews":    {},

	// System overview (read-only)
	"worker.list":    {},
	"sandbox.list":   {},
	"system.stats":   {},
	"security.posture": {},
	"sessions.list": {},

	// Chat / trace (read + send)
	"chat.message":        {},
	"chat.tool_events":    {},
	"chat.thought_events": {},
	"chat.stream_progress": {},

	// Harness (read-only from portal)
	"harness.get": {},

	// Goals / PM entry
	"goal.submit": {},

	// Court actions (require UI confirmation header)
	"proposal.approve": {},
	"proposal.reject":  {},
	"proposal.defer":   {},

	// Agent intervention (require UI confirmation header)
	"agent.pause":   {},
	"agent.resume":  {},
	"agent.cancel":  {},

	// Permissions & visibility (permissions-model.md)
	"permission.list":          {},
	"permission.grant":         {},
	"permission.revoke":        {},
	"permission.check":         {},
	"permission.snapshot":      {},
	"permission.requests.list": {},
	"visibility.list":          {},
	"visibility.get":           {},
	"visibility.set":           {},
}

// HighImpactActions require explicit user confirmation before bridge call.
var HighImpactActions = map[string]struct{}{
	"proposal.approve":      {},
	"proposal.reject":       {},
	"proposal.defer":        {},
	"channel.archive":       {},
	"agent.pause":           {},
	"agent.resume":          {},
	"agent.cancel":          {},
	"channel.remove_member": {},
	"permission.grant":      {},
	"permission.revoke":     {},
	"visibility.set":        {},
}

// IsAllowedBridgeAction reports whether the portal may call action on the bridge.
func IsAllowedBridgeAction(action string) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return false
	}
	_, ok := allowedBridgeActions[action]
	return ok
}

// RequiresConfirmation reports whether the UI must confirm before calling action.
func RequiresConfirmation(action string) bool {
	_, ok := HighImpactActions[action]
	return ok
}

// AllowedBridgeActions returns a sorted copy of the allow-list for tests/docs.
func AllowedBridgeActions() []string {
	out := make([]string, 0, len(allowedBridgeActions))
	for a := range allowedBridgeActions {
		out = append(out, a)
	}
	// simple sort
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}