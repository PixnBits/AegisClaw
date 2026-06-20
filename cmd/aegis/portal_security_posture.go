package main

import (
	"fmt"
	"strings"
	"time"
)

// collectSecurityPostureForPortal returns live security indicators for the portal UI.
func collectSecurityPostureForPortal() map[string]interface{} {
	snap := computeHealthSnapshot(orchestrator)
	indicators := []interface{}{
		map[string]interface{}{
			"id":     "browser_isolated",
			"label":  "Browser isolated",
			"status": "ok",
			"detail": "Portal traffic flows only through the host reverse proxy",
		},
		map[string]interface{}{
			"id":     "no_client_secrets",
			"label":  "No secrets in client",
			"status": "ok",
			"detail": "Actions mediated via vsock bridge and hub ACLs",
		},
		map[string]interface{}{
			"id":     "stomp_scoped",
			"label":  "View-scoped STOMP",
			"status": "ok",
			"detail": "Realtime subscriptions limited per screen",
		},
	}

	if orchestrator != nil {
		if tcb := orchestrator.TCBHealthReport(); tcb != nil {
			if mem, ok := tcb["memory"].(map[string]interface{}); ok {
				within, _ := mem["within_target"].(bool)
				status := "warn"
				if within {
					status = "ok"
				}
				indicators = append(indicators, map[string]interface{}{
					"id":     "daemon_memory",
					"label":  "Daemon memory posture",
					"status": status,
					"detail": fmt.Sprintf("%v MB allocated (target %v MB)", mem["alloc_mb"], mem["target_mb"]),
				})
			}
			if merkle, ok := tcb["merkle_audit"].(map[string]interface{}); ok {
				verify, _ := merkle["verify"].(string)
				status := "ok"
				if verify != "ok" {
					status = "warn"
				}
				indicators = append(indicators, map[string]interface{}{
					"id":     "merkle_audit",
					"label":  "Merkle audit signing",
					"status": status,
					"detail": fmt.Sprintf("signing=%v verify=%v", merkle["signing"], verify),
				})
			}
		}
	}

	storeReady, _ := snap["store_collab_ready"].(bool)
	wpStatus := "ok"
	if errMsg, ok := snap["web_portal"].(string); ok && errMsg != "" {
		if strings.Contains(errMsg, "FAILED") || strings.Contains(errMsg, "not listening") {
			wpStatus = "warn"
		}
	}

	return map[string]interface{}{
		"indicators":            indicators,
		"store_collab_ready":    storeReady,
		"court_personas_online": getMapInt(snap, "court_personas_online"),
		"web_portal":            snap["web_portal"],
		"web_portal_status":     wpStatus,
		"collab":                snap["collab"],
		"updated_at":            time.Now().UTC().Format(time.RFC3339),
	}
}
