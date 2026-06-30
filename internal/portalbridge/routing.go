package portalbridge

import "strings"

// Destination returns the hub component that should handle a portal bridge action.
func Destination(action string) string {
	switch {
	case action == "channel.fanout":
		return "daemon-orchestrator"
	case strings.HasPrefix(action, "sessions."), strings.HasPrefix(action, "team."), strings.HasPrefix(action, "channel."):
		return "store"
	case strings.HasPrefix(action, "skill."), strings.HasPrefix(action, "proposal."):
		return "store"
	case action == "permission.panel", strings.HasPrefix(action, "permission."), strings.HasPrefix(action, "visibility."):
		return "store"
	case action == "tool.registry.discover", action == "audit.list":
		return "store"
	case strings.HasPrefix(action, "ciso.delegation."):
		return "store"
	case strings.HasPrefix(action, "llm."):
		return "store"
	case action == "goal.submit", action == "harness.get":
		return "daemon"
	case strings.HasPrefix(action, "chat."):
		return "agent"
	default:
		return "daemon"
	}
}
