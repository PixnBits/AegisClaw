package collab

import "strings"

// IsChatAgentSession reports whether id is a paired agent-runtime chat session
// (agent-<session> or a bare session slug), as opposed to court personas, PM, or infra VMs.
func IsChatAgentSession(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if strings.HasPrefix(id, "court-persona-") || id == "court-scribe" {
		return false
	}
	if id == "project-manager" || strings.HasPrefix(id, "project-manager-") {
		return false
	}
	for _, infra := range []string{
		"store", "hub", "aegishub", "web-portal", "network-boundary",
		"memory", "builder", "daemon", "daemon-orchestrator",
	} {
		if id == infra {
			return false
		}
	}
	return true
}

// ChatAgentSessionID normalizes an agent card / VM id into the session slug used
// for chat.tool_events and chat.thought_events (strip agent- prefix when present).
func ChatAgentSessionID(agentID string) string {
	if sid := strings.TrimPrefix(agentID, "agent-"); sid != agentID {
		return sid
	}
	return agentID
}