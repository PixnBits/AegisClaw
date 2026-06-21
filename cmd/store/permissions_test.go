package main

import (
	"os"
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
