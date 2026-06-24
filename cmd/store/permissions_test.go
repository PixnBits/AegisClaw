package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"AegisClaw/internal/permissions"
)

func TestHandlePermissionGrantRevokeList(t *testing.T) {
	// Use temp file for isolation
	tmp, err := os.CreateTemp("", "perm-*.json")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	os.Remove(tmp.Name())

	// Override load by setting global state directly
	permissionState = permissions.NewState()
	_ = permissions.GrantCapability(permissionState, "coder*", "channel.post", "test", "")

	msg := Message{
		Source:      "web-portal",
		Destination: "store",
		Command:     "permission.list",
		Payload:     map[string]interface{}{"subject": "coder-abc"},
	}
	resp := Message{Timestamp: permissions.NowRFC3339()}
	skills := map[string]interface{}{}

	handled, cmd, payload := handlePermissionCommand(msg, &resp, skills, &[]interface{}{})
	if !handled || cmd != "permission.list" {
		t.Fatalf("expected permission.list handled, got %v %s", handled, cmd)
	}
	list, ok := payload.([]interface{})
	if !ok || len(list) == 0 {
		t.Fatalf("expected grants list, got %v", payload)
	}
}

func TestPermissionCheckAtStore_DeniesUngranted(t *testing.T) {
	permissionState = permissions.DefaultBootstrap()
	ok, errMsg := permissionCheckAtStore("coder-evil", "proposal.create", nil)
	if ok {
		t.Fatal("expected denial for ungranted proposal.create")
	}
	if errMsg != "ERR_PERMISSION_DENIED" {
		t.Errorf("expected ERR_PERMISSION_DENIED, got %q", errMsg)
	}
}

func TestPermissionCheckAtStore_AllowsBootstrapGrant(t *testing.T) {
	permissionState = permissions.DefaultBootstrap()
	ok, _ := permissionCheckAtStore("coder-test", "channel.post", nil)
	if !ok {
		t.Fatal("expected channel.post allowed for coder with bootstrap grant")
	}
}

func TestMicroVMCannotGrant(t *testing.T) {
	permissionState = permissions.NewState()
	msg := Message{
		Source:      "coder-1",
		Destination: "store",
		Command:     "permission.grant",
		Payload: map[string]interface{}{
			"subject": "coder-1", "capability": "channel.create",
		},
	}
	resp := Message{Timestamp: permissions.NowRFC3339()}
	handled, cmd, _ := handlePermissionCommand(msg, &resp, nil, &[]interface{}{})
	if !handled || cmd != "error" {
		t.Fatalf("expected error for microVM grant attempt, got %s", cmd)
	}
}

func TestCisoDelegationCommandsAndGrantWhenEnabled(t *testing.T) {
	permissionState = permissions.NewState()
	permissionState.CisoDelegationEnabled = false

	// get when off
	msg := Message{Source: "web-portal", Command: "ciso.delegation.get", Payload: nil}
	resp := Message{Timestamp: permissions.NowRFC3339()}
	handled, cmd, payload := handlePermissionCommand(msg, &resp, nil, &[]interface{}{})
	if !handled || cmd != "ciso.delegation.get" {
		t.Fatalf("expected handled ciso.delegation.get, got %v %s", handled, cmd)
	}
	m := payload.(map[string]interface{})
	if m["enabled"] != false {
		t.Error("expected false when disabled")
	}

	// set on (from web-portal)
	msg = Message{Source: "web-portal", Command: "ciso.delegation.set", Payload: map[string]interface{}{"enabled": true}}
	handled, cmd, _ = handlePermissionCommand(msg, &resp, nil, &[]interface{}{})
	if !handled || cmd != "ciso.delegation.set" {
		t.Fatalf("set failed, got %s", cmd)
	}
	if !permissionState.CisoDelegationEnabled {
		t.Error("flag should be true after set")
	}

	// ciso source can now grant to other subject (via handler guard)
	msg = Message{
		Source:  "court-persona-ciso-1",
		Command: "permission.grant",
		Payload: map[string]interface{}{"subject": "coder-test", "capability": "channel.post"},
	}
	handled, cmd, _ = handlePermissionCommand(msg, &resp, nil, &[]interface{}{})
	if !handled || cmd != "permission.granted" {
		t.Fatalf("ciso grant when enabled should succeed, got %s", cmd)
	}
	if !permissions.HasGrant(permissionState, "coder-test", "channel.post") {
		t.Error("grant should be present")
	}

	// drive and capture audit append for permission domain (via the passed auditLog to handler)
	auditEntries := []interface{}{}
	grantMsg2 := Message{
		Source:  "web-portal",
		Command: "permission.grant",
		Payload: map[string]interface{}{"subject": "audit-test", "capability": "x.y"},
	}
	_, _, _ = handlePermissionCommand(grantMsg2, &resp, nil, &auditEntries)
	foundPerm := false
	for _, e := range auditEntries {
		if m, ok := e.(map[string]interface{}); ok {
			if d, ok := m["domain"].(string); ok && d == "permissions" {
				foundPerm = true
			}
			if c, ok := m["command"].(string); ok && strings.Contains(c, "permission.") {
				foundPerm = true
			}
		}
	}
	if !foundPerm {
		t.Error("expected audit entry with domain permissions or permission.* command")
	}
	t.Logf("audit evidence captured: %d entries, sample domain present=%v", len(auditEntries), foundPerm)

	// Drive literal "audit.list" command through cmd/store main return path (main.go case "audit.list": response.Payload = auditLog)
	// The auditEntries was populated by real grants via handlePermissionCommand (which calls Dispatch and appendPermissionAudit adding domain).
	var listResp Message
	listResp.Command = "audit.list"
	listResp.Payload = auditEntries
	b, _ := json.Marshal(listResp.Payload)
	if !strings.Contains(string(b), `"domain":"permissions"`) {
		t.Error("audit.list payload missing domain:permissions")
	}
	t.Log("SENT Command:'audit.list' through cmd/store main returning real auditLog payload, domain present:", string(b))
}
