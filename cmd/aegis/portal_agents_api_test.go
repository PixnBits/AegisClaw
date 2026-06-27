package main

import (
	"testing"
)

// TestHandlePortalDaemonLocal_WorkerList_ReturnsJSONArray guards the bridge contract:
// worker.list must decode as a JSON array for dashboard spaWorkersToAgentCards.
func TestHandlePortalDaemonLocal_WorkerList_ReturnsJSONArray(t *testing.T) {
	out, err := handlePortalDaemonLocal("worker.list", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := out.([]interface{})
	if !ok {
		t.Fatalf("worker.list must return []interface{}, got %T", out)
	}
	_ = list // empty when orchestrator unavailable in unit tests
}

// TestPortalWorkerList_MergesChannelRosterStandby documents that cold/on-demand
// roster members (e.g. project-manager-main) appear even when only court VMs run.
func TestPortalWorkerList_MergesChannelRosterStandby(t *testing.T) {
	workers := []interface{}{
		map[string]interface{}{"id": "court-persona-ciso", "name": "court-persona-ciso", "status": "running"},
	}
	members := []interface{}{
		map[string]interface{}{"role": "project-manager"},
		map[string]interface{}{"role": "court-persona-ciso"},
		map[string]interface{}{"role": "coder"},
	}
	mergeChannelRosterFromMembers(&workers, "main", members)
	if len(workers) < 3 {
		t.Fatalf("expected PM + court + coder roster entries, got %d", len(workers))
	}
	ids := make(map[string]struct{})
	for _, raw := range workers {
		m, _ := raw.(map[string]interface{})
		id, _ := m["id"].(string)
		ids[id] = struct{}{}
	}
	for _, want := range []string{"project-manager-main", "court-persona-ciso", "coder-main"} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing roster worker %q, got %v", want, ids)
		}
	}
}