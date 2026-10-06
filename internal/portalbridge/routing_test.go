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
	}
	for _, action := range storeActions {
		if got := Destination(action); got != "store" {
			t.Errorf("Destination(%q)=%q want store", action, got)
		}
	}
}

func TestDestination_LLMUsageQueriesRouteToStore(t *testing.T) {
	for _, action := range []string{"llm.usage.summary", "llm.usage.recent"} {
		if got := Destination(action); got != "store" {
			t.Errorf("Destination(%q)=%q want store", action, got)
		}
	}
	// The portal must not be able to append usage. A prefix of "llm." or
	// "llm.usage." would route the record command to store.
	for _, action := range []string{"llm.usage.record", "llm.chat", "llm.foo", "llm.usagex"} {
		if got := Destination(action); got == "store" {
			t.Errorf("Destination(%q)=store, want not store", action)
		}
	}
}

func TestDestination_ProjectManagerPermissionsNotDaemonLocal(t *testing.T) {
	// Regression: daemon-local fallback returned {} for permission.list (empty grants in Portal).
	if got := Destination("permission.list"); got == "daemon" {
		t.Fatal("permission.list must not route to daemon local handler")
	}
}
