package permissions

import (
	"fmt"
	"strings"
)

// DispatchCommand handles permission.* / visibility.* / tool.registry.discover / ciso.delegation.*
// It is the single source of truth for mutation, guards, SaveState and audit append.
// Both cmd/store and the e2e fixture call this.
func DispatchCommand(state *State, source, command string, payload map[string]interface{}, audit *[]interface{}, ts string) (respCmd string, resp interface{}, err error) {
	if state == nil {
		state = NewState()
	}

	switch command {
	case "permission.grant":
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		reason, _ := payload["reason"].(string)
		if subject == "" || capability == "" {
			return "error", "subject and capability required", nil
		}
		if IsMicroVMSourcePublic(source) && !AllowsCisoDelegation(source, state.CisoDelegationEnabled) {
			return "", nil, fmt.Errorf("ERR_PERMISSION_DENIED: microVMs cannot grant permissions")
		}
		if e := GrantCapability(state, subject, capability, source, reason); e != nil {
			return "error", e.Error(), nil
		}
		_ = SaveState(state)
		appendPermissionAudit(audit, ts, "permission.grant", source, subject+":"+capability)
		return "permission.granted", map[string]interface{}{"subject": subject, "capability": capability, "version": state.Version}, nil

	case "permission.revoke":
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		if IsMicroVMSourcePublic(source) && !AllowsCisoDelegation(source, state.CisoDelegationEnabled) {
			return "", nil, fmt.Errorf("ERR_PERMISSION_DENIED: microVMs cannot revoke permissions")
		}
		revoked := RevokeCapability(state, subject, capability)
		if revoked {
			_ = SaveState(state)
			appendPermissionAudit(audit, ts, "permission.revoke", source, subject+":"+capability)
		}
		return "permission.revoked", map[string]interface{}{"revoked": revoked, "subject": subject, "capability": capability}, nil

	case "permission.list":
		subject, _ := payload["subject"].(string)
		var list []Grant
		if subject != "" {
			list = ListGrantsForSubject(state, subject)
		} else {
			list = state.Grants
		}
		out := make([]interface{}, len(list))
		for i, g := range list {
			out[i] = g
		}
		return "permission.list", out, nil

	case "permission.check":
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		allowed := HasGrant(state, subject, capability)
		return "permission.check", map[string]interface{}{"allowed": allowed, "subject": subject, "capability": capability}, nil

	case "permission.snapshot":
		subject, _ := payload["subject"].(string)
		if subject == "" {
			subject = source
		}
		snap := BuildSnapshot(state, subject, KnownCapabilities())
		return "permission.snapshot", snap, nil

	case "permission.request":
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		ctx, _ := payload["context"].(string)
		req := RecordRequest(state, subject, capability, ctx)
		_ = SaveState(state)
		appendPermissionAudit(audit, ts, "permission.request", source, subject+":"+capability)
		return "permission.request", req, nil

	case "permission.requests.list":
		subject, _ := payload["subject"].(string)
		var reqs []Request
		if subject != "" {
			reqs = ListRequestsForSubject(state, subject)
		} else {
			reqs = state.Requests
		}
		out := make([]interface{}, len(reqs))
		for i, r := range reqs {
			out[i] = r
		}
		return "permission.requests.list", out, nil

	case "visibility.set":
		subject, _ := payload["subject"].(string)
		capability, _ := payload["capability"].(string)
		levelStr, _ := payload["level"].(string)
		reason, _ := payload["reason"].(string)
		if IsMicroVMSourcePublic(source) && !AllowsCisoDelegation(source, state.CisoDelegationEnabled) {
			return "", nil, fmt.Errorf("ERR_PERMISSION_DENIED: microVMs cannot set visibility")
		}
		SetVisibility(state, subject, capability, VisibilityLevel(levelStr), source, reason)
		_ = SaveState(state)
		appendPermissionAudit(audit, ts, "visibility.set", source, fmt.Sprintf("%s:%s=%s", subject, capability, levelStr))
		return "visibility.set", map[string]interface{}{"subject": subject, "capability": capability, "level": levelStr}, nil

	case "visibility.list":
		subject, _ := payload["subject"].(string)
		var rules []VisibilityRule
		for _, r := range state.Visibility {
			if subject == "" || SubjectMatches(subject, r.Subject) {
				rules = append(rules, r)
			}
		}
		out := make([]interface{}, len(rules))
		for i, r := range rules {
			out[i] = r
		}
		return "visibility.list", out, nil

	case "tool.registry.discover":
		if !state.CisoDelegationEnabled && !HasGrant(state, source, "tool.registry.discover") {
			// simplistic; real uses HasGrant on the flag too
			req := RecordRequest(state, source, "tool.registry.discover", "registry discover without grant")
			_ = SaveState(state)
			return "error", map[string]interface{}{"error": "ERR_PERMISSION_DENIED", "request_id": req.ID}, nil
		}
		snap := BuildSnapshot(state, source, KnownCapabilities())
		// simplified return; caller can filter further
		return "tool.registry.discover", map[string]interface{}{"subject": source, "canDiscover": snap.CanDiscover}, nil

	case "ciso.delegation.get":
		return "ciso.delegation.get", map[string]interface{}{"enabled": state.CisoDelegationEnabled}, nil

	case "ciso.delegation.set":
		if IsCisoSource(source) {
			return "", nil, fmt.Errorf("ERR_PERMISSION_DENIED: ciso sources cannot set the delegation flag")
		}
		enabled, _ := payload["enabled"].(bool)
		state.CisoDelegationEnabled = enabled
		_ = SaveState(state)
		appendPermissionAudit(audit, ts, "ciso.delegation.set", source, fmt.Sprintf("enabled=%v", enabled))
		return "ciso.delegation.set", map[string]interface{}{"enabled": enabled}, nil

	default:
		if len(command) > 11 && (command[:11] == "permission." || command[:11] == "visibility." || command[:5] == "ciso.") {
			return "error", "unknown permission command", nil
		}
		return "", nil, nil // not handled
	}
}

// appendPermissionAudit is the one used by dispatch (moved here for unification).
func appendPermissionAudit(auditLog *[]interface{}, ts, command, source, detail string) {
	if auditLog == nil {
		return
	}
	*auditLog = append(*auditLog, map[string]interface{}{
		"ts":      ts,
		"command": command,
		"source":  source,
		"detail":  detail,
		"domain":  "permissions",
	})
}

// IsCisoSource for dispatch level checks (ciso cannot set delegation).
func IsCisoSource(source string) bool {
	return strings.HasPrefix(source, "court-persona-ciso") || source == "ciso" || strings.HasPrefix(source, "ciso-")
}