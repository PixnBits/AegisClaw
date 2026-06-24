package main

import (
	"encoding/json"

	"AegisClaw/internal/permissions"
)

// permissionState is the durable capability-grant + visibility source of truth.
var permissionState = permissions.DefaultBootstrap()

func initPermissionState() {
	permissionState = permissions.LoadState()
	if permissionState == nil || len(permissionState.Grants) == 0 {
		permissionState = permissions.DefaultBootstrap()
	}
	_ = permissions.SaveState(permissionState)
}

func allCapabilities(skills map[string]interface{}) []string {
	caps := permissions.KnownCapabilities()
	seen := make(map[string]bool)
	for _, c := range caps {
		seen[c] = true
	}
	for id := range skills {
		if !seen[id] {
			caps = append(caps, id)
		}
	}
	return caps
}

func appendPermissionAudit(auditLog *[]interface{}, ts, command, source, detail string) {
	*auditLog = append(*auditLog, map[string]interface{}{
		"ts":      ts,
		"command": command,
		"source":  source,
		"detail":  detail,
		"domain":  "permissions",
	})
}

// handlePermissionCommand is now a thin wrapper over the canonical dispatcher in internal/permissions.
func handlePermissionCommand(msg Message, response *Message, skills map[string]interface{}, auditLog *[]interface{}) (bool, string, interface{}) {
	var p map[string]interface{}
	if msg.Payload != nil {
		if m, ok := msg.Payload.(map[string]interface{}); ok {
			p = m
		}
	}
	respCmd, resp, err := permissions.DispatchCommand(permissionState, msg.Source, msg.Command, p, auditLog, response.Timestamp)
	if err != nil {
		return true, "error", err.Error()
	}
	if respCmd == "" {
		return false, "", nil
	}
	if msg.Command == "tool.registry.discover" {
		caps := allCapabilities(skills)
		snap := permissions.BuildSnapshot(permissionState, msg.Source, caps)
		tools := []interface{}{}
		for id, skill := range skills {
			if snap.VisibleTools[id] || snap.AllowedTools[id] {
				tools = append(tools, skill)
			}
		}
		for _, cap := range caps {
			if (snap.VisibleTools[cap] || snap.AllowedTools[cap]) && skills[cap] == nil {
				tools = append(tools, map[string]interface{}{"name": cap, "description": "capability"})
			}
		}
		return true, "tool.registry.discover", map[string]interface{}{"tools": tools, "subject": msg.Source}
	}
	return true, respCmd, resp
}
// legacy handle kept only for reference during refactor (logic moved to permissions.DispatchCommand)
func handlePermissionCommandLegacy(msg Message, response *Message, skills map[string]interface{}, auditLog *[]interface{}) (bool, string, interface{}) {
	// intentionally empty - use DispatchCommand instead
	return false, "", nil
}
func permissionCheckAtStore(source, command string, skills map[string]interface{}) (bool, string) {
	if !permissions.IsCapabilityCommand(command) {
		return true, ""
	}
	if permissions.HasGrant(permissionState, source, command) {
		return true, ""
	}
	// Wildcard grants: check persona pattern
	pattern := permissions.PersonaPattern(source)
	if permissions.HasGrant(permissionState, pattern, command) {
		return true, ""
	}
	req := permissions.RecordRequest(permissionState, source, command, "denied at store enforcement")
	_ = permissions.SaveState(permissionState)
	_ = req
	return false, "ERR_PERMISSION_DENIED"
}

// export permission state for tests
func exportPermissionStateJSON() string {
	b, _ := json.Marshal(permissionState)
	return string(b)
}
