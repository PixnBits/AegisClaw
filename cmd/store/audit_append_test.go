package main

import (
	"encoding/json"
	"os"
	"testing"
)

// audit.append is a one-way hub push (#156): Store records the entry and
// sends no reply, since audit.appended is not granted back to the sender.
func TestAuditAppendRecordsWithoutReply(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	for i, src := range []string{"network-boundary", "builder"} {
		resp := Message{Timestamp: "2026-10-06T00:00:00Z"}
		skip := dispatchStoreCommand(Message{
			Source: src, Destination: "store", Command: "audit.append",
			Payload: map[string]interface{}{"action": "blocked_request", "n": float64(i)},
		}, &resp, w)
		if !skip {
			t.Fatalf("audit.append from %s: skipReply = false (reply %q), want true", src, resp.Command)
		}
		if resp.Command != "" {
			t.Fatalf("audit.append from %s set reply command %q", src, resp.Command)
		}
	}
	if n := len(*w.auditLog); n != 2 {
		t.Fatalf("audit log has %d entries, want 2", n)
	}
	raw, err := os.ReadFile("audit.json")
	if err != nil {
		t.Fatalf("audit.json not written: %v", err)
	}
	var saved []map[string]interface{}
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) != 2 || saved[0]["action"] != "blocked_request" {
		t.Fatalf("audit.json = %s (%v)", raw, err)
	}

	// audit.list still replies and sees the entries.
	resp := Message{}
	if skip := dispatchStoreCommand(Message{Source: "daemon-internal", Command: "audit.list"}, &resp, w); skip || resp.Command != "audit.list" {
		t.Fatalf("audit.list skip=%v command=%q", skip, resp.Command)
	}
}
