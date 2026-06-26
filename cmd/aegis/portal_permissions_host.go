package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const hostPermissionRPCTimeout = 20 * time.Second

// handleHostAgentPermissions serves /api/agents/{id}/permissions on the host daemon
// proxy. Store permission reads/writes go through daemon-internal → Hub → store
// (reliable unix path). The web-portal guest bridge is a poor fit for batched store
// RPCs (inverted hub bridge serializes JSON and aborts sessions on timeout).
func handleHostAgentPermissions(w http.ResponseWriter, r *http.Request, agentID string) {
	ctx, cancel := context.WithTimeout(r.Context(), hostPermissionRPCTimeout)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		out, err := hostFetchPermissionPanel(ctx, agentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(out) //nolint:errcheck
	case http.MethodPost:
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		action, _ := body["action"].(string)
		capability, _ := body["capability"].(string)
		if r.Header.Get("X-Aegis-Confirmed") != "1" {
			switch action {
			case "grant", "revoke", "hide":
				http.Error(w, "confirmation required", http.StatusPreconditionRequired)
				return
			}
		}
		switch action {
		case "grant":
			_, err := sendToComponentViaHubContext(ctx, "store", "permission.grant", map[string]interface{}{
				"subject": agentID, "capability": capability, "reason": body["reason"],
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "revoke":
			_, err := sendToComponentViaHubContext(ctx, "store", "permission.revoke", map[string]interface{}{
				"subject": agentID, "capability": capability,
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "hide":
			_, err := sendToComponentViaHubContext(ctx, "store", "visibility.set", map[string]interface{}{
				"subject": agentID, "capability": capability, "level": "hidden", "reason": body["reason"],
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "action": action}) //nolint:errcheck
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseAgentPermissionsPath(path string) (agentID string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/agents/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[1] == "permissions" && parts[0] != "" {
		return parts[0], true
	}
	return "", false
}

func hostFetchPermissionPanel(ctx context.Context, agentID string) (map[string]interface{}, error) {
	// Fast path: batched store command when the guest store image includes permission.panel.
	panelCtx, panelCancel := context.WithTimeout(ctx, 5*time.Second)
	panel, panelErr := sendToComponentViaHubContext(panelCtx, "store", "permission.panel", map[string]interface{}{"subject": agentID})
	panelCancel()
	if panelErr == nil {
		if m, ok := panel.(map[string]interface{}); ok {
			return hostFormatPermissionPanel(agentID, m), nil
		}
		panelErr = fmt.Errorf("permission.panel: unexpected payload type %T", panel)
	}
	grants, gErr := sendToComponentViaHubContext(ctx, "store", "permission.list", map[string]interface{}{"subject": agentID})
	requests, _ := sendToComponentViaHubContext(ctx, "store", "permission.requests.list", map[string]interface{}{"subject": agentID})
	visibility, _ := sendToComponentViaHubContext(ctx, "store", "visibility.list", map[string]interface{}{"subject": agentID})
	snapshot, _ := sendToComponentViaHubContext(ctx, "store", "permission.snapshot", map[string]interface{}{"subject": agentID})
	if gErr != nil {
		if panelErr != nil {
			return nil, panelErr
		}
		return nil, gErr
	}
	return hostFormatPermissionPanel(agentID, map[string]interface{}{
		"grants":     grants,
		"requests":   requests,
		"visibility": visibility,
		"snapshot":   snapshot,
	}), nil
}

func hostFormatPermissionPanel(agentID string, m map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"agent_id":   agentID,
		"grants":     hostNormalizePermissionList(m["grants"]),
		"requests":   hostNormalizePermissionList(m["requests"]),
		"visibility": hostNormalizePermissionList(m["visibility"]),
		"snapshot":   hostNormalizePermissionSnapshot(m["snapshot"]),
	}
}

func hostNormalizePermissionList(v interface{}) interface{} {
	if v == nil {
		return []interface{}{}
	}
	if arr, ok := v.([]interface{}); ok {
		return arr
	}
	if m, ok := v.(map[string]interface{}); ok && len(m) == 0 {
		return []interface{}{}
	}
	return v
}

func hostNormalizePermissionSnapshot(v interface{}) interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return v
}