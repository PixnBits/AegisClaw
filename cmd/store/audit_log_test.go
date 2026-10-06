package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resetAuditState(t *testing.T) {
	t.Helper()
	reset := func() {
		auditAppendFailedTotal.Store(0)
		auditCoalescedTotal.Store(0)
		storeAuditCoalescer = newAuditCoalescer()
		auditDiskMu.Lock()
		auditDisk = map[string]*auditDiskState{}
		auditDiskMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func captureSecurityLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := securityLogWriter
	securityLogWriter = func() io.Writer { return buf }
	t.Cleanup(func() { securityLogWriter = orig })
	return buf
}

func auditEntries(n int, tag string) []interface{} {
	var out []interface{}
	for i := 0; i < n; i++ {
		out = append(out, map[string]interface{}{"command": "proposal.create", "source": tag, "n": float64(i)})
	}
	return out
}

func assertMode0600(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode %v, want 0600", path, fi.Mode().Perm())
	}
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, ".audit-*.tmp"))
	if len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

// The first save writes the whole log atomically at 0600; later saves only
// append the new entries to the same file. The root over the loaded log
// matches the in-memory one.
func TestAuditSaveAtomicThenAppendOnly(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	log := auditEntries(3, "a")
	if err := saveAuditToFile(auditLogFile, log); err != nil {
		t.Fatal(err)
	}
	assertMode0600(t, auditLogFile)
	first, _ := os.ReadFile(auditLogFile)
	if strings.Count(string(first), "\n") != 3 {
		t.Fatalf("want 3 JSON lines, got %q", first)
	}
	fi1, _ := os.Stat(auditLogFile)

	log = append(log, auditEntries(2, "b")...)
	if err := saveAuditToFile(auditLogFile, log); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(auditLogFile)
	if !os.SameFile(fi1, fi2) {
		t.Fatal("an append replaced the file; want an in-place append")
	}
	second, _ := os.ReadFile(auditLogFile)
	if !bytes.HasPrefix(second, first) || strings.Count(string(second), "\n") != 5 {
		t.Fatalf("append rewrote earlier entries:\n%q\n%q", first, second)
	}
	// Saving again with nothing new writes nothing.
	if err := saveAuditToFile(auditLogFile, log); err != nil {
		t.Fatal(err)
	}
	if third, _ := os.ReadFile(auditLogFile); !bytes.Equal(third, second) {
		t.Fatal("a save with no new entries changed the file")
	}

	loaded := loadAuditFromFile(auditLogFile)
	if computeMerkleRoot(loaded) != computeMerkleRoot(log) || len(loaded) != 5 {
		t.Fatalf("root changed across load: %d entries", len(loaded))
	}
	noTempFiles(t, ".")
}

// A log shorter than what was persisted (or a file whose state is unknown)
// is rewritten atomically, not appended to.
func TestAuditSaveRewritesWhenStateUnknown(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	if err := saveAuditToFile(auditLogFile, auditEntries(4, "a")); err != nil {
		t.Fatal(err)
	}
	fi1, _ := os.Stat(auditLogFile)
	if err := saveAuditToFile(auditLogFile, auditEntries(2, "z")); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(auditLogFile)
	if os.SameFile(fi1, fi2) {
		t.Fatal("a shorter log was appended; want an atomic rewrite")
	}
	if got := loadAuditFromFile(auditLogFile); len(got) != 2 || !strings.Contains(fmt.Sprint(got), "z") {
		t.Fatalf("after rewrite: %v", got)
	}
}

// A failed write returns an error, leaves no temp file, bumps
// audit.append_failed, writes one SECURITY line, and the next good save
// repairs the file with the whole log.
func TestAuditWriteFailureReportedAndRepaired(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	logs := captureSecurityLog(t)
	log := auditEntries(2, "a")
	// A good save first, so the failure below hits the append path.
	if err := saveAuditToFile(auditLogFile, log[:1]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(auditLogFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(auditLogFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveAuditToFile(auditLogFile, log); err == nil {
		t.Fatal("append onto a directory returned nil")
	}

	persistAudit(log, Message{Source: "network-boundary", Command: "audit.append"})
	if n := auditAppendFailedTotal.Load(); n != 1 {
		t.Fatalf("audit.append_failed = %d, want 1", n)
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], securityEventPrefix) {
		t.Fatalf("security log = %q", logs.String())
	}
	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[0], securityEventPrefix)), &ev); err != nil {
		t.Fatal(err)
	}
	if ev["event"] != storeAuditAppendFailedEvent || ev["source"] != "network-boundary" || ev["failed_total"] != float64(1) {
		t.Fatalf("event = %#v", ev)
	}

	// store.security_stats reports the counter.
	resp := Message{}
	handleStoreSecurityStats(Message{Source: "daemon-internal-1", Command: storeSecurityStatsCommand}, &resp, newPanicDeduper(4, time.Minute))
	payload, _ := resp.Payload.(map[string]interface{})
	af, _ := payload[auditAppendFailedStat].(map[string]interface{})
	if af == nil || af["total"] != uint64(1) {
		t.Fatalf("store.security_stats %s = %#v", auditAppendFailedStat, payload[auditAppendFailedStat])
	}

	// Fix the path; the next save rewrites everything, including entries
	// added since the failure.
	if err := os.Remove(auditLogFile); err != nil {
		t.Fatal(err)
	}
	log = append(log, auditEntries(1, "b")...)
	if err := saveAuditToFile(auditLogFile, log); err != nil {
		t.Fatal(err)
	}
	if got := loadAuditFromFile(auditLogFile); len(got) != 3 {
		t.Fatalf("after repair: %d entries, want 3", len(got))
	}
}

// The atomic path fails cleanly too: an error and no temp file left.
func TestAuditAtomicWriteFailureCleansUp(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	if err := os.Mkdir(auditLogFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveAuditToFile(auditLogFile, auditEntries(2, "a")); err == nil {
		t.Fatal("atomic write onto a directory returned nil")
	}
	noTempFiles(t, ".")
}

// A last line cut off by a crash is dropped on load and the next save
// replaces the file atomically. A bad line in the middle is kept as a
// marker, so the root shows it.
func TestAuditLoadTornTailAndBadLine(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	good, _ := marshalAuditLines(auditEntries(2, "a"))
	if err := os.WriteFile(auditLogFile, append(good, []byte(`{"command":"proposal.cre`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadAuditFromFile(auditLogFile)
	if len(got) != 2 {
		t.Fatalf("torn tail: %d entries, want 2", len(got))
	}
	fi1, _ := os.Stat(auditLogFile)
	got = append(got, auditEntries(1, "b")...)
	if err := saveAuditToFile(auditLogFile, got); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(auditLogFile)
	if os.SameFile(fi1, fi2) {
		t.Fatal("save after a torn tail appended; want an atomic rewrite")
	}
	if again := loadAuditFromFile(auditLogFile); len(again) != 3 {
		t.Fatalf("after repair: %d entries", len(again))
	}

	resetAuditState(t)
	bad := append(append([]byte{}, good...), []byte("not json\n")...)
	bad = append(bad, good...)
	if err := os.WriteFile(auditLogFile, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	got = loadAuditFromFile(auditLogFile)
	if len(got) != 5 || !strings.Contains(fmt.Sprint(got[2]), "unparseable line") {
		t.Fatalf("bad middle line: %v", got)
	}
}

// An older audit.json (one JSON array) is migrated to audit.jsonl at 0600
// with the same entries and root.
func TestAuditLegacyMigration(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	legacy := auditEntries(3, "old")
	b, _ := json.Marshal(legacy)
	if err := os.WriteFile(legacyAuditLogFile, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadAuditLog()
	if len(got) != 3 || computeMerkleRoot(got) != computeMerkleRoot(legacy) {
		t.Fatalf("migrated %d entries", len(got))
	}
	assertMode0600(t, auditLogFile)
	if again := loadAuditFromFile(auditLogFile); computeMerkleRoot(again) != computeMerkleRoot(legacy) {
		t.Fatal("migrated file has a different root")
	}
	// Once audit.jsonl exists it wins over audit.json.
	resetAuditState(t)
	if err := os.WriteFile(legacyAuditLogFile, []byte(`[{"x":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadAuditLog(); len(got) != 3 {
		t.Fatalf("audit.jsonl not preferred: %v", got)
	}
}

// An existing audit file with a wider mode is tightened to 0600 on append.
func TestAuditAppendTightensMode(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	good, _ := marshalAuditLines(auditEntries(1, "a"))
	if err := os.WriteFile(auditLogFile, good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(auditLogFile, 0o644); err != nil {
		t.Fatal(err)
	}
	log := loadAuditFromFile(auditLogFile)
	log = append(log, auditEntries(1, "b")...)
	if err := saveAuditToFile(auditLogFile, log); err != nil {
		t.Fatal(err)
	}
	assertMode0600(t, auditLogFile)
}

func TestCapAuditPayload(t *testing.T) {
	long := strings.Repeat("x", 5000)
	got := capAuditPayload(map[string]interface{}{"action": "blocked_request", "url": long, "reason": long, "skill_id": "s"}).(map[string]interface{})
	if len(got["url"].(string)) != auditFieldCap || len(got["reason"].(string)) != auditFieldCap || got["skill_id"] != "s" {
		t.Fatalf("fields not capped: url=%d reason=%d", len(got["url"].(string)), len(got["reason"].(string)))
	}
	if tf, _ := got["truncated_fields"].([]string); len(tf) != 2 || tf[0] != "reason" || tf[1] != "url" {
		t.Fatalf("truncated_fields = %v", got["truncated_fields"])
	}
	// Many small fields can't add up past the entry cap.
	big := map[string]interface{}{"action": "blocked_request"}
	for i := 0; i < 200; i++ {
		big[fmt.Sprintf("k%03d", i)] = strings.Repeat("y", 100)
	}
	if m := capAuditPayload(big).(map[string]interface{}); m["oversized"] != true || m["action"] != "blocked_request" {
		t.Fatalf("oversized entry = %v", m)
	}
	if s := capAuditPayload(long).(string); len(s) != auditFieldCap {
		t.Fatalf("string payload %d bytes", len(s))
	}
	if b, _ := json.Marshal(capAuditPayload([]interface{}{long, long})); len(b) > auditEntryMaxBytes+64 {
		t.Fatalf("array payload %d bytes", len(b))
	}
	small := map[string]interface{}{"action": "push_allowed"}
	if m := capAuditPayload(small).(map[string]interface{}); len(m) != 1 || m["action"] != "push_allowed" {
		t.Fatalf("small payload changed: %v", m)
	}
}

// The stored entry carries the hub-verified source and the Store's time,
// whatever the payload claims; the payload is kept under "entry".
func TestAuditAppendWrapsSourceAndTime(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	w := testStoreWorld(t)
	before := time.Now().UTC()
	resp := Message{}
	dispatchStoreCommand(Message{
		Source: "network-boundary", Destination: "store", Command: "audit.append",
		Payload: map[string]interface{}{"action": "proxied_request", "source": "store", "received_at": "1999-01-01T00:00:00Z", "url": "https://x.example"},
	}, &resp, w)
	if len(*w.auditLog) != 1 {
		t.Fatalf("audit log has %d entries", len(*w.auditLog))
	}
	e := (*w.auditLog)[0].(map[string]interface{})
	if e["source"] != "network-boundary" || e["command"] != "audit.append" {
		t.Fatalf("entry = %#v", e)
	}
	ts, err := time.Parse(time.RFC3339Nano, e["received_at"].(string))
	if err != nil || ts.Before(before.Add(-time.Second)) || ts.After(time.Now().Add(time.Second)) {
		t.Fatalf("received_at = %v (%v)", e["received_at"], err)
	}
	inner := e["entry"].(map[string]interface{})
	if inner["source"] != "store" || inner["received_at"] != "1999-01-01T00:00:00Z" || inner["url"] != "https://x.example" {
		t.Fatalf("payload not kept under entry: %#v", inner)
	}
	// And it reached disk.
	if got := loadAuditFromFile(auditLogFile); len(got) != 1 || !strings.Contains(fmt.Sprint(got), "network-boundary") {
		t.Fatalf("on disk: %v", got)
	}
}

// Identical blocked_request events inside the window are counted, not
// appended; the next one after the window carries repeats_suppressed.
// Other actions and other skills/urls/sources aren't coalesced.
func TestAuditCoalescesBlockedRequests(t *testing.T) {
	resetAuditState(t)
	c := newAuditCoalescer()
	t0 := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
	blocked := func(src, skill, url string) Message {
		return Message{Source: src, Command: "audit.append", Payload: map[string]interface{}{"action": "blocked_request", "skill_id": skill, "url": url}}
	}
	appended := 0
	for i := 0; i < 5; i++ {
		if _, ok := auditAppendEntry(c, blocked("network-boundary", "s1", "https://a"), t0.Add(time.Duration(i)*time.Second)); ok {
			appended++
		}
	}
	if appended != 1 || auditCoalescedTotal.Load() != 4 {
		t.Fatalf("appended %d, coalesced %d; want 1 and 4", appended, auditCoalescedTotal.Load())
	}
	for _, m := range []Message{
		blocked("network-boundary", "s2", "https://a"),
		blocked("network-boundary", "s1", "https://b"),
		blocked("builder", "s1", "https://a"),
	} {
		if _, ok := auditAppendEntry(c, m, t0.Add(10*time.Second)); !ok {
			t.Fatalf("distinct event coalesced: %#v", m.Payload)
		}
	}
	for i := 0; i < 3; i++ {
		if _, ok := auditAppendEntry(c, Message{Source: "network-boundary", Payload: map[string]interface{}{"action": "proxied_request", "skill_id": "s1", "url": "https://a"}}, t0); !ok {
			t.Fatal("non-blocked action coalesced")
		}
	}
	e, ok := auditAppendEntry(c, blocked("network-boundary", "s1", "https://a"), t0.Add(auditCoalesceWindow))
	if !ok || e["repeats_suppressed"] != uint64(4) {
		t.Fatalf("after window: ok=%v entry=%#v", ok, e)
	}
	if _, ok := auditAppendEntry(c, blocked("network-boundary", "s1", "https://a"), t0.Add(auditCoalesceWindow+time.Second)); ok {
		t.Fatal("new window not started")
	}
}

func TestAuditCoalescerBounded(t *testing.T) {
	resetAuditState(t)
	c := newAuditCoalescer()
	t0 := time.Now()
	for i := 0; i < auditCoalesceMaxKeys+50; i++ {
		c.admit(fmt.Sprint("k", i), t0)
	}
	if n := c.tracked(); n != auditCoalesceMaxKeys {
		t.Fatalf("tracked %d keys, want %d", n, auditCoalesceMaxKeys)
	}
	// The oldest key was evicted, so it is admitted again; a recent one isn't.
	if ok, _ := c.admit("k0", t0); !ok {
		t.Fatal("evicted key not admitted")
	}
	if ok, _ := c.admit(fmt.Sprint("k", auditCoalesceMaxKeys+49), t0); ok {
		t.Fatal("recent key admitted inside its window")
	}
	// A touched key survives eviction.
	c2 := newAuditCoalescer()
	for i := 0; i < auditCoalesceMaxKeys; i++ {
		c2.admit(fmt.Sprint("k", i), t0)
	}
	c2.admit("k0", t0)
	c2.admit("new", t0)
	if ok, _ := c2.admit("k0", t0); ok {
		t.Fatal("recently used key was evicted")
	}
	if ok, _ := c2.admit("k1", t0); !ok {
		t.Fatal("least recently used key was kept")
	}
}

func TestStoreSecurityStatsReportsAuditCounters(t *testing.T) {
	resetAuditState(t)
	auditAppendFailedTotal.Store(2)
	auditCoalescedTotal.Store(9)
	resp := Message{}
	handleStoreSecurityStats(Message{Source: "daemon-internal", Command: storeSecurityStatsCommand}, &resp, newPanicDeduper(4, time.Minute))
	p, _ := resp.Payload.(map[string]interface{})
	if p[storeHandlerPanicEventName] == nil {
		t.Fatal("panic block missing")
	}
	if af, _ := p[auditAppendFailedStat].(map[string]interface{}); af == nil || af["total"] != uint64(2) {
		t.Fatalf("%s = %#v", auditAppendFailedStat, p[auditAppendFailedStat])
	}
	if co, _ := p[auditCoalescedStat].(map[string]interface{}); co == nil || co["total"] != uint64(9) || co["max_keys"] != auditCoalesceMaxKeys {
		t.Fatalf("%s = %#v", auditCoalescedStat, p[auditCoalescedStat])
	}
}

// A JSON-array file saved back to the same path is rewritten as JSON lines,
// never appended to.
func TestAuditLegacyArraySavedInPlaceIsRewritten(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	b, _ := json.Marshal(auditEntries(2, "old"))
	if err := os.WriteFile("legacy.json", b, 0o600); err != nil {
		t.Fatal(err)
	}
	log := loadAuditFromFile("legacy.json")
	log = append(log, auditEntries(1, "new")...)
	if err := saveAuditToFile("legacy.json", log); err != nil {
		t.Fatal(err)
	}
	resetAuditState(t)
	if got := loadAuditFromFile("legacy.json"); len(got) != 3 || computeMerkleRoot(got) != computeMerkleRoot(log) {
		t.Fatalf("after save: %v", got)
	}
}

// State-changing commands reach the audit file through the shipped
// post-switch block, with no explicit save by the caller.
func TestStateChangeAuditPersisted(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	resetAuditState(t)
	var log []interface{}
	msg := Message{Source: "client", Command: "proposal.create"}
	resp := Message{Command: "proposal.created", Payload: map[string]interface{}{"proposal_id": "p1"}, Timestamp: time.Now().UTC().Format(time.RFC3339)}
	appendAuditForStateChangeIfNeeded(msg, &resp, &log)
	got := loadAuditFromFile(auditLogFile)
	if len(got) != 1 || !strings.Contains(fmt.Sprint(got[0]), "proposal.create") {
		t.Fatalf("on disk: %v", got)
	}
	if resp.Payload.(map[string]interface{})["merkle_root"] != computeMerkleRoot(got) {
		t.Fatal("reply root differs from the root over the persisted log")
	}
}
