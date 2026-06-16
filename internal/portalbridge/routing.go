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
	case strings.HasPrefix(action, "chat."):
		return "agent"
	default:
		return "daemon"
	}
}
