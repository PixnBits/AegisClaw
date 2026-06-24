package permissions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDispatchCommand_Table(t *testing.T) {
	state := DefaultBootstrap()
	state.CisoDelegationEnabled = false

	// web-portal can set delegation
	_, resp, err := DispatchCommand(state, "web-portal", "ciso.delegation.set", map[string]interface{}{"enabled": true}, &[]interface{}{}, NowRFC3339())
	if err != nil {
		t.Fatalf("set delegation: %v", err)
	}
	m := resp.(map[string]interface{})
	if m["enabled"] != true {
		t.Error("expected enabled true")
	}
	if !state.CisoDelegationEnabled {
		t.Error("flag not set")
	}

	// ciso* source can grant when enabled
	_, resp, err = DispatchCommand(state, "court-persona-ciso-1", "permission.grant", map[string]interface{}{"subject": "coder-test", "capability": "channel.post"}, &[]interface{}{}, NowRFC3339())
	if err != nil {
		t.Fatalf("ciso grant when enabled: %v", err)
	}

	// ciso source on set is allowed by dispatch (ACL is separate outer gate)
	// but to test denial at dispatch for set from ciso we can document; here we test via flag
	state.CisoDelegationEnabled = false
	_, resp, err = DispatchCommand(state, "court-persona-ciso-1", "ciso.delegation.set", map[string]interface{}{"enabled": true}, &[]interface{}{}, NowRFC3339())
	// dispatch denies ciso.set for ciso sources (IsCisoSource); ACL is outer gate in real paths
	// To simulate ACL deny for ciso.set we test the guard in other place; for dispatch we just check it mutates when called.
	_ = resp

	// after grant, audit slice has domain
	audit := []interface{}{}
	DispatchCommand(state, "web-portal", "permission.grant", map[string]interface{}{"subject": "audit-c", "capability": "x.y"}, &audit, NowRFC3339())
	found := false
	for _, e := range audit {
		if m, ok := e.(map[string]interface{}); ok {
			if d, ok := m["domain"].(string); ok && d == "permissions" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected domain:permissions in audit after grant")
	}

	// Note: audit.list drive through the real cmd/store main return of accumulated auditLog (with domain from append) is tested in cmd/store/permissions_test.go.
	// Here we only validate that grant appends produce the domain entry on the slice (shipped appendPermissionAudit path).

	// ciso grant denied when flag off (fresh state)
	state2 := DefaultBootstrap()
	state2.CisoDelegationEnabled = false
	isM := IsMicroVMSourcePublic("court-persona-ciso-1")
	allows := AllowsCisoDelegation("court-persona-ciso-1", state2.CisoDelegationEnabled)
	t.Logf("DEBUG isMicro=%v allows=%v flag=%v", isM, allows, state2.CisoDelegationEnabled)
	_, _, err = DispatchCommand(state2, "court-persona-ciso-1", "permission.grant", map[string]interface{}{"subject": "coder-test", "capability": "secret.thing"}, &[]interface{}{}, NowRFC3339())
	if err == nil {
		t.Error("expected deny for ciso grant when delegation disabled")
	}

	// ciso.set from ciso source denied at dispatch (even if flag on)
	state3 := DefaultBootstrap()
	state3.CisoDelegationEnabled = true
	_, _, err = DispatchCommand(state3, "court-persona-ciso-1", "ciso.delegation.set", map[string]interface{}{"enabled": false}, &[]interface{}{}, NowRFC3339())
	if err == nil {
		t.Error("expected deny for ciso.set from ciso source at dispatch")
	}
}

// Test that the shipped append path used by Dispatch (and thus by store main for permission commands) produces domain entries on the slice passed for audit.
func TestAppendProducesDomainForAudit(t *testing.T) {
	state := DefaultBootstrap()
	state.CisoDelegationEnabled = true
	audit := []interface{}{}
	DispatchCommand(state, "web-portal", "permission.grant", map[string]interface{}{"subject": "a1", "capability": "b.c"}, &audit, NowRFC3339())

	b, _ := json.Marshal(audit)
	if !strings.Contains(string(b), `"domain":"permissions"`) {
		t.Error("appended audit entry from real Dispatch path must contain domain:permissions")
	}
}