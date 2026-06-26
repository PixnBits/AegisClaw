package permissions

import "testing"

func TestDispatchPermissionPanel(t *testing.T) {
	st := DefaultBootstrap()
	_ = GrantCapability(st, "court-persona-user-advocate", "channel.post", "test", "bootstrap")

	_, resp, err := DispatchCommand(st, "web-portal", "permission.panel", map[string]interface{}{
		"subject": "court-persona-user-advocate",
	}, &[]interface{}{}, NowRFC3339())
	if err != nil {
		t.Fatal(err)
	}
	m, ok := resp.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", resp)
	}
	grants, ok := m["grants"].([]interface{})
	if !ok || len(grants) == 0 {
		t.Fatalf("expected grants, got %v", m["grants"])
	}
	if _, ok := m["snapshot"]; !ok {
		t.Fatal("expected snapshot in panel")
	}
}