//go:build linux

package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestHubPermissionListLive probes store permission.list via the daemon-internal hub
// path when a real daemon is running (AEGIS_HUB_SOCKET or ~/.aegis/hub.sock).
func TestHubPermissionListLive(t *testing.T) {
	if os.Getenv("AEGIS_PERM_PROBE") == "" {
		t.Skip("set AEGIS_PERM_PROBE=1 with daemon running to probe live hub")
	}
	type probe struct{ cmd string; payload interface{} }
	probes := []probe{
		{"channel.list", nil},
		{"skill.list", nil},
		{"permission.list", map[string]interface{}{"subject": "project-manager-main"}},
		{"permission.snapshot", map[string]interface{}{"subject": "project-manager-main"}},
		{"permission.panel", map[string]interface{}{"subject": "project-manager-main"}},
	}
	for _, p := range probes {
		pctx, pcancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp, err := sendToComponentViaHubContext(pctx, "store", p.cmd, p.payload)
		pcancel()
		t.Logf("%s: err=%v resp_type=%T", p.cmd, err, resp)
	}
}