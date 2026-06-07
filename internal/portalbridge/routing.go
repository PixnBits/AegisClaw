package portalbridge

import "strings"

// Destination returns the hub component that should handle a portal bridge action.
func Destination(action string) string {
	switch {
	case strings.HasPrefix(action, "sessions."), strings.HasPrefix(action, "team."), strings.HasPrefix(action, "channel."):
		return "store"
	case strings.HasPrefix(action, "chat."):
		return "agent"
	default:
		return "daemon"
	}
}
