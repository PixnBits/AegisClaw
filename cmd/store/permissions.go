package main

import (
	"encoding/json"
	"fmt"
	"strings"

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

// handlePermissionCommand processes permission.* / visibility.* / tool.registry.discover.
// Returns (handled, responseCommand, responsePayload).
func handlePermissionCommand(msg Message, response *Message, skills map[string]interface{}, auditLog *[]interface{}) (bool, string, interface{}) {
	caps := allCapabilities(skills)

	switch msg.Command {
	case "permission.grant":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		reason, _ := payload["reason"].(string)
		if subject == "" || capability == "" {
			return true, "error", "subject and capability required"
		}
		if permissions.IsMicroVMSourcePublic(msg.Source) && !permissions.AllowsCisoDelegation(msg.Source, permissionState.CisoDelegationEnabled) {
			return true, "error", "ERR_PERMISSION_DENIED: microVMs cannot grant permissions"
		}
		if err := permissions.GrantCapability(permissionState, subject, capability, msg.Source, reason); err != nil {
			return true, "error", err.Error()
		}
		_ = permissions.SaveState(permissionState)
		appendPermissionAudit(auditLog, response.Timestamp, "permission.grant", msg.Source, subject+":"+capability)
		return true, "permission.granted", map[string]interface{}{"subject": subject, "capability": capability, "version": permissionState.Version}

	case "permission.revoke":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		if permissions.IsMicroVMSourcePublic(msg.Source) && !permissions.AllowsCisoDelegation(msg.Source, permissionState.CisoDelegationEnabled) {
			return true, "error", "ERR_PERMISSION_DENIED: microVMs cannot revoke permissions"
		}
		revoked := permissions.RevokeCapability(permissionState, subject, capability)
		if revoked {
			_ = permissions.SaveState(permissionState)
			appendPermissionAudit(auditLog, response.Timestamp, "permission.revoke", msg.Source, subject+":"+capability)
		}
		return true, "permission.revoked", map[string]interface{}{"revoked": revoked, "subject": subject, "capability": capability}

	case "permission.list":
		payload, _ := msg.Payload.(map[string]interface{})
		subject, _ := payload["subject"].(string)
		var list []permissions.Grant
		if subject != "" {
			list = permissions.ListGrantsForSubject(permissionState, subject)
		} else {
			list = permissionState.Grants
		}
		out := make([]interface{}, len(list))
		for i, g := range list {
			out[i] = g
		}
		return true, "permission.list", out

	case "permission.check":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		allowed := permissions.HasGrant(permissionState, subject, capability)
		return true, "permission.check", map[string]interface{}{"allowed": allowed, "subject": subject, "capability": capability}

	case "permission.snapshot":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		if subject == "" {
			subject = msg.Source
		}
		snap := permissions.BuildSnapshot(permissionState, subject, caps)
		return true, "permission.snapshot", snap

	case "permission.request":
		// Store-side recording of denied attempts (from Hub or Portal)
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		ctx, _ := payload["context"].(string)
		req := permissions.RecordRequest(permissionState, subject, capability, ctx)
		_ = permissions.SaveState(permissionState)
		appendPermissionAudit(auditLog, response.Timestamp, "permission.request", msg.Source, subject+":"+capability)
		return true, "permission.request", req

	case "permission.requests.list":
		payload, _ := msg.Payload.(map[string]interface{})
		subject, _ := payload["subject"].(string)
		var reqs []permissions.Request
		if subject != "" {
			reqs = permissions.ListRequestsForSubject(permissionState, subject)
		} else {
			reqs = permissionState.Requests
		}
		out := make([]interface{}, len(reqs))
		for i, r := range reqs {
			out[i] = r
		}
		return true, "permission.requests.list", out

	case "ciso.delegation.get":
		return true, "ciso.delegation.get", map[string]interface{}{"enabled": permissionState.CisoDelegationEnabled}

	case "ciso.delegation.set":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		enabled, _ := payload["enabled"].(bool)
		// ACLs gate who may call this (user/portal); store just persists
		permissionState.CisoDelegationEnabled = enabled
		_ = permissions.SaveState(permissionState)
		appendPermissionAudit(auditLog, response.Timestamp, "ciso.delegation.set", msg.Source, fmt.Sprintf("enabled=%v", enabled))
		return true, "ciso.delegation.set", map[string]interface{}{"enabled": enabled}

	case "visibility.set":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		levelStr, _ := payload["level"].(string)
		reason, _ := payload["reason"].(string)
		if permissions.IsMicroVMSourcePublic(msg.Source) && !permissions.AllowsCisoDelegation(msg.Source, permissionState.CisoDelegationEnabled) {
			return true, "error", "ERR_PERMISSION_DENIED: microVMs cannot set visibility"
		}
		permissions.SetVisibility(permissionState, subject, capability, permissions.VisibilityLevel(levelStr), msg.Source, reason)
		_ = permissions.SaveState(permissionState)
		appendPermissionAudit(auditLog, response.Timestamp, "visibility.set", msg.Source, fmt.Sprintf("%s:%s=%s", subject, capability, levelStr))
		return true, "visibility.set", map[string]interface{}{"subject": subject, "capability": capability, "level": levelStr}

	case "visibility.get":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return true, "error", "invalid payload"
		}
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		for _, r := range permissionState.Visibility {
			if r.Subject == subject && r.Capability == capability {
				return true, "visibility.get", r
			}
		}
		return true, "visibility.get", nil

	case "visibility.list":
		payload, _ := msg.Payload.(map[string]interface{})
		subject, _ := payload["subject"].(string)
		var rules []permissions.VisibilityRule
		for _, r := range permissionState.Visibility {
			if subject == "" || permissions.SubjectMatches(subject, r.Subject) {
				rules = append(rules, r)
			}
		}
		out := make([]interface{}, len(rules))
		for i, r := range rules {
			out[i] = r
		}
		return true, "visibility.list", out

	case "tool.registry.discover":
		subject := msg.Source
		if !permissions.HasGrant(permissionState, subject, "tool.registry.discover") {
			req := permissions.RecordRequest(permissionState, subject, "tool.registry.discover", "registry discover without grant")
			_ = permissions.SaveState(permissionState)
			return true, "error", map[string]interface{}{"error": "ERR_PERMISSION_DENIED", "request_id": req.ID}
		}
		snap := permissions.BuildSnapshot(permissionState, subject, caps)
		// Return visibility-filtered registry (redacted descriptions for hidden internals)
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
		return true, "tool.registry.discover", map[string]interface{}{"tools": tools, "subject": subject}

	default:
		if strings.HasPrefix(msg.Command, "permission.") || strings.HasPrefix(msg.Command, "visibility.") {
			return true, "error", "unknown permission command"
		}
		return false, "", nil
	}
}

// permissionCheckAtStore verifies capability before Store handles sensitive commands.
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
