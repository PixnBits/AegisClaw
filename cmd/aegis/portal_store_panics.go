package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// storeSecurityStatsFn asks the Store for store.security_stats (host-only on
// the Store side). Tests replace it.
var storeSecurityStatsFn = func() (interface{}, error) {
	if orchestrator == nil {
		return nil, errors.New("daemon not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	return sendToComponentViaHubContext(ctx, "store", "store.security_stats", nil)
}

// storeHandlerPanicPosture returns the security-posture indicator for
// recovered Store handler panics, plus the raw counts (nil when the Store
// can't be asked). Any panic is "warn": checked decoding (#134) should make
// them unreachable, so one means a handler still trusts bad input or someone
// is probing.
func storeHandlerPanicPosture() (indicator map[string]interface{}, counts map[string]interface{}) {
	indicator = map[string]interface{}{
		"id":     "store_handler_panic",
		"label":  "Store handler panics",
		"status": "unknown",
		"detail": "Store counters unavailable",
	}
	data, err := storeSecurityStatsFn()
	if err != nil {
		indicator["detail"] = "Store counters unavailable: " + err.Error()
		return indicator, nil
	}
	m, _ := data.(map[string]interface{})
	hp, ok := m["store.handler_panic"].(map[string]interface{})
	if !ok {
		indicator["detail"] = "Store counters unavailable: unexpected reply"
		return indicator, nil
	}
	total := getMapInt(hp, "total")
	hashes := getMapInt(hp, "tracked_hashes")
	if total == 0 {
		indicator["status"] = "ok"
		indicator["detail"] = "No recovered handler panics since Store start"
	} else {
		indicator["status"] = "warn"
		indicator["detail"] = fmt.Sprintf("%d recovered handler panic(s), %d distinct stack(s); see SECURITY lines in the Store log", total, hashes)
	}
	return indicator, hp
}
