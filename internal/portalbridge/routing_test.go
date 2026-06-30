package portalbridge

import "testing"

func TestDestination_PermissionsRouteToStore(t *testing.T) {
	storeActions := []string{
		"permission.list",
		"permission.panel",
		"permission.grant",
		"permission.snapshot",
		"permission.requests.list",
		"visibility.list",
		"visibility.set",
		"tool.registry.discover",
		"ciso.delegation.get",
		"ciso.delegation.set",
		"audit.list",
		"llm.usage.summary",
		"llm.usage.record",
		"llm.usage.recent",
	}
	for _, action := range storeActions {
		if got := Destination(action); got != "store" {
			t.Errorf("Destination(%q)=%q want store", action, got)
		}
	}

	// llm.* queries/records must reach store (not daemon local fallback which returns empty).
	for _, action := range []string{"llm.usage.summary", "llm.usage.record", "llm.usage.recent"} {
		if got := Destination(action); got != "store" {
			t.Errorf("Destination(%q)=%q want store", action, got)
		}
	}
}

func TestDestination_ProjectManagerPermissionsNotDaemonLocal(t *testing.T) {
	// Regression: daemon-local fallback returned {} for permission.list (empty grants in Portal).
	if got := Destination("permission.list"); got == "daemon" {
		t.Fatal("permission.list must not route to daemon local handler")
	}
}