package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// withTempDir runs fn after chdir into a fresh temp dir and restores cwd on return.
// This makes the relative-file load/save helpers (grants.json etc.) hermetic per test.
func withTempDir(t *testing.T, fn func()) {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to temp: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()
	fn()
}

func TestScheduleCancelListTimers(t *testing.T) {
	withTempDir(t, func() {
		id := "timer-42"
		expires := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
		meta := map[string]interface{}{
			"session_id": "sess-1",
			"preset":     "full",
			"expires":    expires,
		}
		if err := ScheduleTimer(id, meta); err != nil {
			t.Fatalf("ScheduleTimer: %v", err)
		}

		// File should exist with 0600 perms (paranoid security)
		fi, err := os.Stat("timers.json")
		if err != nil {
			t.Fatalf("timers.json not created: %v", err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Errorf("timers.json perms = %04o, want 0600", fi.Mode().Perm())
		}

		list := ListActiveTimers()
		found := false
		for _, got := range list {
			if got == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ListActiveTimers did not return %s: %v", id, list)
		}

		CancelTimer(id)
		list2 := ListActiveTimers()
		for _, got := range list2 {
			if got == id {
				t.Errorf("CancelTimer did not remove %s", id)
			}
		}
	})
}

func TestReconcileExpiredTimers(t *testing.T) {
	withTempDir(t, func() {
		now := time.Now().UTC()
		expiredID := "expired-timer"
		futureID := "future-timer"

		// Seed directly (bypasses ScheduleTimer for controlled timestamps)
		timers := map[string]interface{}{
			expiredID: map[string]interface{}{
				"session_id": "s1",
				"expires":    now.Add(-1 * time.Hour).Format(time.RFC3339), // past
			},
			futureID: map[string]interface{}{
				"session_id": "s2",
				"expires":    now.Add(1 * time.Hour).Format(time.RFC3339), // future
			},
		}
		b, _ := json.MarshalIndent(timers, "", "  ")
		_ = os.WriteFile("timers.json", b, 0600)

		expired := reconcileExpiredTimers()
		if len(expired) != 1 || expired[0] != expiredID {
			t.Errorf("reconcileExpiredTimers returned %v, want [%s]", expired, expiredID)
		}

		// Future timer must still be present
		remaining := ListActiveTimers()
		if len(remaining) != 1 || remaining[0] != futureID {
			t.Errorf("after reconcile, remaining = %v, want only %s", remaining, futureID)
		}
	})
}

func TestReconcileExpiredGrantsAndBackground(t *testing.T) {
	withTempDir(t, func() {
		now := time.Now().UTC().Format(time.RFC3339)
		past := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)

		grants := map[string]interface{}{
			"grant-alive": map[string]interface{}{"expires": now}, // treat as non-expired for this run
			"grant-dead":  map[string]interface{}{"expires": past},
		}
		b, _ := json.MarshalIndent(grants, "", "  ")
		_ = os.WriteFile("grants.json", b, 0600)

		bg := map[string]interface{}{
			"bg-dead": map[string]interface{}{"expires": past},
		}
		b, _ = json.MarshalIndent(bg, "", "  ")
		_ = os.WriteFile("background.json", b, 0600)

		expA := ReconcileExpiredAutonomy()
		expB := ReconcileExpiredBackgroundWork()

		if len(expA) != 1 {
			t.Errorf("ReconcileExpiredAutonomy got %d, want 1 (the dead one)", len(expA))
		}
		if len(expB) != 1 {
			t.Errorf("ReconcileExpiredBackgroundWork got %d, want 1", len(expB))
		}

		// Verify disk side-effect (dead entries removed)
		afterGrants := loadGrants()
		if _, ok := afterGrants["grant-dead"]; ok {
			t.Error("grant-dead should have been removed from grants.json")
		}
	})
}

// TestRecoveryOnStartup simulates the boot-time catch-up path added in 2.4.
// We write expired data, call the reconcile functions directly (as the recovery code does),
// and assert both the return values and that the files were cleaned.
func TestRecoveryOnStartup(t *testing.T) {
	withTempDir(t, func() {
		past := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
		future := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)

		// Seed all three collections with a mix
		_ = os.WriteFile("grants.json", []byte(`{"old-grant":{"expires":"`+past+`"},"live-grant":{"expires":"`+future+`"}}`), 0600)
		_ = os.WriteFile("background.json", []byte(`{"old-bg":{"expires":"`+past+`"}}`), 0600)
		_ = os.WriteFile("timers.json", []byte(`{"old-timer":{"expires":"`+past+`"},"live-timer":{"expires":"`+future+`"}}`), 0600)

		a := ReconcileExpiredAutonomy()
		b := ReconcileExpiredBackgroundWork()
		tm := reconcileExpiredTimers()

		if len(a) != 1 || a[0] != "old-grant" {
			t.Errorf("recovery autonomy: got %v", a)
		}
		if len(b) != 1 || b[0] != "old-bg" {
			t.Errorf("recovery background: got %v", b)
		}
		if len(tm) != 1 || tm[0] != "old-timer" {
			t.Errorf("recovery timers: got %v", tm)
		}

		// Future items survive
		if len(ListActiveTimers()) != 1 {
			t.Error("live timer should have survived recovery reconcile")
		}
	})
}

func TestFilePerms0600(t *testing.T) {
	withTempDir(t, func() {
		ScheduleTimer("perm-test", map[string]interface{}{"expires": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})

		for _, name := range []string{"timers.json", "grants.json", "background.json"} {
			// grants/background may not exist yet in this test; create via their save if needed
			if name == "grants.json" {
				saveGrants(map[string]interface{}{"x": map[string]interface{}{"expires": "2099-01-01T00:00:00Z"}})
			}
			if name == "background.json" {
				saveBackgroundWork(map[string]interface{}{"y": map[string]interface{}{"expires": "2099-01-01T00:00:00Z"}})
			}

			fi, err := os.Stat(name)
			if err != nil {
				// some files may legitimately not exist in a given sub-test; skip
				continue
			}
			if fi.Mode().Perm() != 0600 {
				t.Errorf("%s has perms %04o, want 0600", name, fi.Mode().Perm())
			}
		}
	})
}