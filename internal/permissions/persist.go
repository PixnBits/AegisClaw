package permissions

import (
	"encoding/json"
	"os"
)

const permissionsFile = "permissions.json"

// LoadState reads durable permission state from disk.
// Returns DefaultBootstrap only when the file is missing or corrupt.
// A persisted file with zero grants is preserved (intentional deny-all).
func LoadState() *State {
	data, err := os.ReadFile(permissionsFile)
	if err != nil {
		return DefaultBootstrap()
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultBootstrap()
	}
	return &s
}

// SaveState persists permission state (0600).
func SaveState(s *State) error {
	bytes, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(permissionsFile, bytes, 0600)
}

// KnownCapabilities returns the global capability registry for filtering.
// In production this is merged with Store skill registry at runtime.
func KnownCapabilities() []string {
	return []string{
		"tool.list", "tool.search", "tool.registry.discover",
		"channel.create", "channel.list", "channel.get", "channel.join", "channel.post",
		"channel.get_relevant_since", "channel.get_messages",
		"memory.store", "memory.query", "memory.get_context",
		"llm.call", "proposal.create", "proposal.submit", "proposal.list",
		"permission.grant", "permission.revoke", "permission.list", "permission.check",
		"visibility.set", "visibility.get", "visibility.list",
		"court.review", "secrets.push",
		"discord_monitor.send_message", "discord_monitor.get_recent",
		"web_research.search", "web_research.summarize_url",
	}
}

// IsCapabilityCommand reports whether a command string is a fine-grained capability
// subject to permission grants (noun.verb pattern, excluding hub control commands).
func IsCapabilityCommand(cmd string) bool {
	if cmd == "" || cmd == "register" || cmd == "response" || cmd == "ack" ||
		cmd == "version" || cmd == "get-version" || cmd == "ping" || cmd == "pong" {
		return false
	}
	// Safe discovery commands are always allowed locally
	if cmd == "tool.list" || cmd == "tool.search" {
		return false
	}
	// Collaboration delivery / event routing — ACL-gated, not capability-granted
	switch cmd {
	case "channel.activity", "channel.member_notify", "channel.updated",
		"channel.relay_activity", "channel.fanout", "channel.posted",
		"chat.message", "chat.tool_events", "chat.thought_events", "chat.stream_progress",
		"user.goal", "user.turn",
		"ensure.role", "orchestrator.ensure_role":
		return false
	}
	// Permission management commands are ACL-gated, not capability-granted
	if len(cmd) > 11 && (cmd[:11] == "permission." || cmd[:11] == "visibility.") {
		return false
	}
	for i, c := range cmd {
		if c == '.' {
			return i > 0 && i < len(cmd)-1
		}
	}
	return false
}
