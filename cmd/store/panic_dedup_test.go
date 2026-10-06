package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"
)

//go:noinline
func panicAtSameSite(m map[string]int, n *int) {
	m["x"] = *n // m is nil: "assignment to entry in nil map"
}

// stackFromSameSite returns debug.Stack() from a recovered panic at one
// fixed site, called with fresh pointer arguments on a new goroutine.
func stackFromSameSite(n int) []byte {
	var out []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if recover() != nil {
				out = debug.Stack()
			}
		}()
		v := new(int)
		*v = n
		panicAtSameSite(nil, v)
	}()
	wg.Wait()
	return out
}

//go:noinline
func panicAtOtherSite() {
	var s []int
	_ = s[3]
}

func stackFromOtherSite() (out []byte) {
	defer func() {
		if recover() != nil {
			out = debug.Stack()
		}
	}()
	panicAtOtherSite()
	return nil
}

func TestStackSignatureSamePanicSameHash(t *testing.T) {
	a, b := stackFromSameSite(1), stackFromSameSite(2)
	if bytes.Equal(a, b) {
		t.Fatal("raw stacks are identical; the test no longer shows why normalizing is needed")
	}
	if sha256.Sum256(a) == sha256.Sum256(b) {
		t.Fatal("raw stack hashes match; expected goroutine ids/args to differ")
	}
	if stackSignature(a) != stackSignature(b) {
		t.Fatalf("same panic site, different signatures:\n%s\n----\n%s", normalizeStack(a), normalizeStack(b))
	}
	if stackSignature(a) == stackSignature(stackFromOtherSite()) {
		t.Fatal("different panic sites must not share a signature")
	}
}

func TestNormalizeStackFixture(t *testing.T) {
	in := "goroutine 42 [running]:\n" +
		"runtime/debug.Stack()\n" +
		"\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n" +
		"main.(*storeWorld).handle(0xc000123450, {0xc0000a8000, 0x10}, 0x1)\n" +
		"\t/src/cmd/store/store_dispatch.go:612 +0x1a4\n" +
		"panic({0x5f2e40?, 0x6a1b30?})\n" +
		"\t/usr/local/go/src/runtime/panic.go:785 +0x132\n" +
		"created by main.serve in goroutine 7\n" +
		"\t/src/cmd/store/main.go:800 +0x2c5\n"
	want := "goroutine\n" +
		"runtime/debug.Stack(...)\n" +
		"\t/usr/local/go/src/runtime/debug/stack.go:26\n" +
		"main.(*storeWorld).handle(...)\n" +
		"\t/src/cmd/store/store_dispatch.go:612\n" +
		"panic(...)\n" +
		"\t/usr/local/go/src/runtime/panic.go:785\n" +
		"created by main.serve\n" +
		"\t/src/cmd/store/main.go:800\n"
	if got := string(normalizeStack([]byte(in))); got != want {
		t.Fatalf("normalizeStack:\n%s\nwant:\n%s", got, want)
	}
	// The same trace from another goroutine, other pointers and a moved pc
	// offset normalizes to the same thing.
	in2 := strings.NewReplacer("goroutine 42", "goroutine 9001", "0xc000123450", "0xc0009f0000",
		"+0x1a4", "+0x1a8", "in goroutine 7", "in goroutine 77").Replace(in)
	if stackSignature([]byte(in)) != stackSignature([]byte(in2)) {
		t.Fatal("goroutine id / pointer / offset changes must not change the signature")
	}
	// A different line number is a different site.
	in3 := strings.Replace(in, "store_dispatch.go:612", "store_dispatch.go:613", 1)
	if stackSignature([]byte(in)) == stackSignature([]byte(in3)) {
		t.Fatal("a different file:line must change the signature")
	}
}

func decodeOne(t *testing.T, buf *bytes.Buffer) storeSecurityEvent {
	t.Helper()
	evs := securityLines(t, buf.String())
	buf.Reset()
	if len(evs) != 1 {
		t.Fatalf("want 1 SECURITY line, got %d", len(evs))
	}
	return evs[0]
}

func TestLogRecoveredHandlerPanicDedup(t *testing.T) {
	d := newPanicDeduper(8, time.Minute)
	stack := []byte("goroutine 1 [running]:\nmain.h(0x1)\n\t/src/x.go:10 +0x1\n" + strings.Repeat("s", 20<<10))
	t0 := time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	msg := Message{Command: "channel.post", Source: "agent-x"}
	longPanic := strings.Repeat("p", 2000)

	logRecoveredHandlerPanic(&buf, d, msg, longPanic, stack, t0)
	first := decodeOne(t, &buf)
	if first.Stack == "" || first.StackOmitted || first.RepeatCount != 1 || first.PanicTotal != 1 {
		t.Fatalf("first occurrence must log the full stack: %+v", first)
	}
	if len(first.Stack) != securityStackCap || len(first.Panic) != securityPanicCap {
		t.Fatalf("first line caps: stack=%d panic=%d", len(first.Stack), len(first.Panic))
	}

	for i, off := range []time.Duration{time.Second, 30 * time.Second, 59 * time.Second} {
		logRecoveredHandlerPanic(&buf, d, msg, longPanic, stack, t0.Add(off))
		raw := buf.String()
		ev := decodeOne(t, &buf)
		if ev.Stack != "" || !ev.StackOmitted || strings.Contains(raw, `"stack":`) {
			t.Fatalf("repeat %d within the window must be hash-only: %q", i, raw)
		}
		if ev.StackSHA256 != first.StackSHA256 {
			t.Fatalf("repeat hash %q != first %q", ev.StackSHA256, first.StackSHA256)
		}
		if ev.RepeatCount != uint64(i+2) || ev.PanicTotal != uint64(i+2) {
			t.Fatalf("repeat %d: repeat_count=%d panic_total=%d", i, ev.RepeatCount, ev.PanicTotal)
		}
		if len(ev.Panic) > securityFieldCap || ev.Command != "channel.post" || ev.Source != "agent-x" {
			t.Fatalf("hash-only line fields: panic=%d %+v", len(ev.Panic), ev)
		}
		if len(raw) > 1024 {
			t.Fatalf("hash-only line is %d bytes", len(raw))
		}
	}

	// Window passed: full stack again, and the count keeps going.
	logRecoveredHandlerPanic(&buf, d, msg, longPanic, stack, t0.Add(61*time.Second))
	again := decodeOne(t, &buf)
	if again.Stack == "" || again.StackOmitted || again.RepeatCount != 5 {
		t.Fatalf("after the window the full stack must log again: omitted=%v count=%d", again.StackOmitted, again.RepeatCount)
	}
	// And the window restarts from that full line.
	logRecoveredHandlerPanic(&buf, d, msg, "p", stack, t0.Add(90*time.Second))
	if ev := decodeOne(t, &buf); !ev.StackOmitted || ev.RepeatCount != 6 {
		t.Fatalf("window must restart at the last full line: %+v", ev)
	}

	// A different stack is its own signature: full stack, count 1, total 7.
	other := []byte("goroutine 1 [running]:\nmain.other(0x1)\n\t/src/y.go:20 +0x1\n")
	logRecoveredHandlerPanic(&buf, d, msg, "q", other, t0.Add(91*time.Second))
	if ev := decodeOne(t, &buf); ev.StackOmitted || ev.RepeatCount != 1 || ev.PanicTotal != 7 {
		t.Fatalf("new signature: %+v", ev)
	}
}

func TestDispatchWithPanicGuardDedupsRealPanics(t *testing.T) {
	buf := withPanicGuardSeams(t, func(m Message, _ *Message, _ *storeWorld) bool {
		n := len(m.Command)
		panicAtSameSite(nil, &n)
		return false
	})
	for i := 0; i < 3; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func(i int) { // new goroutine id and new pointers each time
			defer wg.Done()
			dispatchWithPanicGuard(Message{Command: fmt.Sprintf("channel.post.%d", i), Source: "agent-x"}, &Message{}, &storeWorld{})
		}(i)
		wg.Wait()
	}
	evs := securityLines(t, buf.String())
	if len(evs) != 3 {
		t.Fatalf("want 3 lines, got %d", len(evs))
	}
	if evs[0].Stack == "" || evs[0].RepeatCount != 1 {
		t.Fatalf("first real panic must carry the stack: %+v", evs[0].RepeatCount)
	}
	for i := 1; i < 3; i++ {
		if !evs[i].StackOmitted || evs[i].Stack != "" || evs[i].RepeatCount != uint64(i+1) || evs[i].StackSHA256 != evs[0].StackSHA256 {
			t.Fatalf("real repeat %d: omitted=%v count=%d hash=%s vs %s", i, evs[i].StackOmitted, evs[i].RepeatCount, evs[i].StackSHA256, evs[0].StackSHA256)
		}
	}
	if st := storePanicDedup.snapshot(panicStatsTopN); st.Total != 3 || st.TrackedHashes != 1 {
		t.Fatalf("counters after 3 real panics: %+v", st)
	}
}

func TestPanicDeduperBounded(t *testing.T) {
	d := newPanicDeduper(4, time.Minute)
	t0 := time.Unix(0, 0)
	for i := 0; i < 100; i++ {
		d.observe(fmt.Sprintf("h%03d", i), t0.Add(time.Duration(i)*time.Second))
		if len(d.byHash) > 4 || d.lru.Len() > 4 {
			t.Fatalf("after %d hashes: map=%d lru=%d, cap 4", i+1, len(d.byHash), d.lru.Len())
		}
	}
	st := d.snapshot(16)
	if st.Total != 100 || st.TrackedHashes != 4 || st.EvictedHashes != 96 || st.MaxHashes != 4 {
		t.Fatalf("snapshot: %+v", st)
	}
	// LRU: h096 is the oldest. Touching it keeps it; the next new hash
	// evicts h097 (now the least recently seen) instead.
	d.observe("h096", t0.Add(200*time.Second))
	d.observe("new", t0.Add(201*time.Second))
	if _, ok := d.byHash["h096"]; !ok {
		t.Fatal("recently seen hash was evicted")
	}
	if _, ok := d.byHash["h097"]; ok {
		t.Fatal("least recently seen hash was kept")
	}
	// An evicted hash that comes back is new again: full stack, count 1.
	if full, count, _ := d.observe("h000", t0.Add(202*time.Second)); !full || count != 1 {
		t.Fatalf("returning evicted hash: full=%v count=%d", full, count)
	}
}

func TestPanicDeduperSnapshotTopN(t *testing.T) {
	d := newPanicDeduper(64, time.Minute)
	t0 := time.Unix(0, 0)
	for i := 0; i < 20; i++ {
		for j := 0; j <= i; j++ {
			d.observe(fmt.Sprintf("h%02d", i), t0)
		}
	}
	st := d.snapshot(16)
	if len(st.ByHash) != 16 || !st.ByHashTruncate || st.TrackedHashes != 20 || st.Total != 210 {
		t.Fatalf("snapshot: rows=%d truncated=%v tracked=%d total=%d", len(st.ByHash), st.ByHashTruncate, st.TrackedHashes, st.Total)
	}
	if st.ByHash[0].StackSHA256 != "h19" || st.ByHash[0].Count != 20 || st.ByHash[15].Count != 5 {
		t.Fatalf("rows not sorted by count: first=%+v last=%+v", st.ByHash[0], st.ByHash[15])
	}
}

func TestStoreSecurityStatsCommand(t *testing.T) {
	old := storePanicDedup
	storePanicDedup = newPanicDeduper(panicDedupMaxHashes, panicFullStackInterval)
	t.Cleanup(func() { storePanicDedup = old })
	storePanicDedup.observe("aaaa", time.Now())
	storePanicDedup.observe("aaaa", time.Now())
	storePanicDedup.observe("bbbb", time.Now())

	for _, src := range []string{"daemon-internal", "daemon-internal-3", "aegis-cli-internal-123", "daemon-orchestrator"} {
		resp := Message{}
		if skip := dispatchStoreCommand(Message{Source: src, Destination: "store", Command: "store.security_stats"}, &resp, &storeWorld{auditLog: &[]interface{}{}}); skip {
			t.Fatalf("%s: skipReply", src)
		}
		if resp.Command != "store.security_stats" {
			t.Fatalf("%s: reply %q %v", src, resp.Command, resp.Payload)
		}
		// What the daemon receives after the JSON round trip.
		b, _ := json.Marshal(resp.Payload)
		var got map[string]map[string]interface{}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		hp := got["store.handler_panic"]
		if hp["total"] != float64(3) || hp["tracked_hashes"] != float64(2) {
			t.Fatalf("%s: counts %v", src, hp)
		}
		rows, _ := hp["by_hash"].([]interface{})
		if len(rows) != 2 || rows[0].(map[string]interface{})["count"] != float64(2) {
			t.Fatalf("%s: by_hash %v", src, rows)
		}
	}
	for _, src := range []string{"agent-x", "court-persona-ciso", "web-portal", "builder-1", "", "daemon-internalx", "store"} {
		resp := Message{}
		dispatchStoreCommand(Message{Source: src, Destination: "store", Command: "store.security_stats"}, &resp, &storeWorld{auditLog: &[]interface{}{}})
		if resp.Command != "error" || !strings.Contains(fmt.Sprint(resp.Payload), "ERR_PERMISSION_DENIED") {
			t.Fatalf("%q must be denied: %q %v", src, resp.Command, resp.Payload)
		}
	}
}
