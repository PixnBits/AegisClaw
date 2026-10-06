package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func withStoreSecurityStats(t *testing.T, fn func() (interface{}, error)) {
	t.Helper()
	old := storeSecurityStatsFn
	storeSecurityStatsFn = fn
	t.Cleanup(func() { storeSecurityStatsFn = old })
}

// storeStatsReply is what sendToComponentViaHubContext hands back: the
// Store's payload after a JSON round trip.
func storeStatsReply(t *testing.T, raw string) func() (interface{}, error) {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return func() (interface{}, error) { return v, nil }
}

func postureIndicator(t *testing.T, posture map[string]interface{}, id string) map[string]interface{} {
	t.Helper()
	inds, _ := posture["indicators"].([]interface{})
	for _, it := range inds {
		if m, ok := it.(map[string]interface{}); ok && m["id"] == id {
			return m
		}
	}
	t.Fatalf("indicator %q missing from %v", id, inds)
	return nil
}

func TestSecurityPostureShowsStoreHandlerPanics(t *testing.T) {
	withStoreSecurityStats(t, storeStatsReply(t, `{"store.handler_panic":{"total":7,"tracked_hashes":2,"evicted_hashes":0,"max_hashes":128,
		"by_hash":[{"stack_sha256":"aa","count":5},{"stack_sha256":"bb","count":2}]}}`))
	posture := collectSecurityPostureForPortal()
	ind := postureIndicator(t, posture, "store_handler_panic")
	if ind["status"] != "warn" {
		t.Fatalf("status = %v, want warn when panics > 0", ind["status"])
	}
	if d, _ := ind["detail"].(string); !strings.Contains(d, "7 recovered") || !strings.Contains(d, "2 distinct") {
		t.Fatalf("detail = %q", d)
	}
	counts, ok := posture["store_handler_panic"].(map[string]interface{})
	if !ok || counts["total"] != float64(7) {
		t.Fatalf("store_handler_panic counts = %#v", posture["store_handler_panic"])
	}
	if rows, _ := counts["by_hash"].([]interface{}); len(rows) != 2 {
		t.Fatalf("per-hash counts not passed through: %v", counts["by_hash"])
	}
	// The posture reaches the portal as JSON.
	if _, err := json.Marshal(posture); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityPostureStoreHandlerPanicsZeroAndUnavailable(t *testing.T) {
	withStoreSecurityStats(t, storeStatsReply(t, `{"store.handler_panic":{"total":0,"tracked_hashes":0,"by_hash":[]}}`))
	if ind := postureIndicator(t, collectSecurityPostureForPortal(), "store_handler_panic"); ind["status"] != "ok" {
		t.Fatalf("zero panics: status %v", ind["status"])
	}

	withStoreSecurityStats(t, func() (interface{}, error) { return nil, errors.New("hub: store not registered") })
	posture := collectSecurityPostureForPortal()
	ind := postureIndicator(t, posture, "store_handler_panic")
	if ind["status"] != "unknown" || !strings.Contains(ind["detail"].(string), "store not registered") {
		t.Fatalf("unavailable: %v", ind)
	}
	if m, _ := posture["store_handler_panic"].(map[string]interface{}); m != nil {
		t.Fatalf("counts must be nil when unavailable: %#v", posture["store_handler_panic"])
	}

	withStoreSecurityStats(t, storeStatsReply(t, `{"something":"else"}`))
	if ind := postureIndicator(t, collectSecurityPostureForPortal(), "store_handler_panic"); ind["status"] != "unknown" {
		t.Fatalf("unexpected reply shape: %v", ind)
	}
}
