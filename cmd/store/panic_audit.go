package main

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// securityEventPrefix starts every Store security event line. Normal error
// lines start with "store:". Operators and log shippers can match on
// "SECURITY " to route these separately. The rest of the line is one JSON
// object, so attacker-controlled fields (command, source, panic text) can't
// break the line or forge a second event.
const securityEventPrefix = "SECURITY "

// storeHandlerPanicEventName is the event name for a recovered handler panic.
// Checked payload decoding (#134) should make this unreachable. If it shows
// up, a handler still trusts input it shouldn't, and someone may be probing.
const storeHandlerPanicEventName = "store.handler_panic"

const (
	securityFieldCap = 256
	securityPanicCap = 1024
	securityStackCap = 16 << 10

	// panicFullStackInterval is how often one stack signature may log its
	// full stack. Between full lines, repeats log hash-only.
	panicFullStackInterval = time.Minute
	// panicDedupMaxHashes caps how many signatures are remembered. The
	// least recently seen one is dropped first. A dropped signature that
	// comes back is treated as new and logs its full stack again.
	panicDedupMaxHashes = 128
	// panicStatsTopN caps the per-hash list in store.security_stats.
	panicStatsTopN = 16
)

type storeSecurityEvent struct {
	Event       string `json:"event"`
	Severity    string `json:"severity"`
	Time        string `json:"time"`
	Command     string `json:"command"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Panic       string `json:"panic"`
	StackSHA256 string `json:"stack_sha256"`
	// RepeatCount is how many times this stack signature has been seen by
	// this Store process, including this one (1 = first).
	RepeatCount uint64 `json:"repeat_count"`
	// PanicTotal is every recovered handler panic so far, all signatures.
	PanicTotal uint64 `json:"panic_total"`
	// StackOmitted marks a hash-only line: the full stack for this
	// signature was logged less than panicFullStackInterval ago.
	StackOmitted bool   `json:"stack_omitted,omitempty"`
	Stack        string `json:"stack,omitempty"`
}

// securityLogWriter is where security events go. It's the standard logger's
// output (stderr, the same stream the VM console captures), written without
// the log package's timestamp prefix so the line stays parseable. Tests
// replace it.
var securityLogWriter = func() io.Writer { return log.Writer() }

// storeDispatch is the dispatcher dispatchWithPanicGuard calls. Tests replace
// it to force a panic.
var storeDispatch = dispatchStoreCommand

// dispatchWithPanicGuard runs one command. If the handler panics, it logs a
// SECURITY event and turns the reply into a generic error. The panic text
// stays out of the reply. The caller holds mu, and this returns normally so
// mu.Unlock and the sign/encode path still run.
func dispatchWithPanicGuard(msg Message, response *Message, w *storeWorld) (skipReply bool) {
	defer func() {
		if rec := recover(); rec != nil {
			logRecoveredHandlerPanic(securityLogWriter(), storePanicDedup, msg, rec, debug.Stack(), time.Now())
			response.Command = "error"
			response.Payload = fmt.Sprintf("internal error handling %s", truncateUTF8(msg.Command, securityFieldCap))
			skipReply = false
		}
	}()
	return storeDispatch(msg, response, w)
}

// logRecoveredHandlerPanic writes one SECURITY line for a recovered panic.
// The first time a stack signature appears, and then at most once per
// panicFullStackInterval, the line carries the full (capped) stack. Other
// repeats are hash-only, with a repeat count and a shorter panic text, so a
// probing loop can't fill the host console log with 16 KiB stacks.
func logRecoveredHandlerPanic(out io.Writer, d *panicDeduper, msg Message, rec interface{}, stack []byte, now time.Time) {
	hash := stackSignature(stack)
	full, count, total := d.observe(hash, now)
	ev := storeSecurityEvent{
		Event:       storeHandlerPanicEventName,
		Severity:    "security",
		Time:        now.UTC().Format(time.RFC3339Nano),
		Command:     truncateUTF8(msg.Command, securityFieldCap),
		Source:      truncateUTF8(msg.Source, securityFieldCap),
		Destination: truncateUTF8(msg.Destination, securityFieldCap),
		StackSHA256: hash,
		RepeatCount: count,
		PanicTotal:  total,
	}
	if full {
		ev.Panic = truncateUTF8(fmt.Sprint(rec), securityPanicCap)
		ev.Stack = truncateUTF8(string(stack), securityStackCap)
	} else {
		ev.Panic = truncateUTF8(fmt.Sprint(rec), securityFieldCap)
		ev.StackOmitted = true
	}
	line, err := json.Marshal(ev)
	if err != nil {
		// Not reachable with string fields. Still emit a marked line.
		line = []byte(`{"event":"` + storeHandlerPanicEventName + `","severity":"security","marshal_error":true}`)
	}
	_, _ = io.WriteString(out, securityEventPrefix+string(line)+"\n")
}

var (
	stackGoroutineRe = regexp.MustCompile(`^goroutine \d+`)
	stackCreatedInRe = regexp.MustCompile(` in goroutine \d+$`)
	stackOffsetRe    = regexp.MustCompile(` \+0x[0-9a-f]+$`)
)

// normalizeStack removes the parts of a debug.Stack() trace that change
// from one call to the next even when the code path is the same: goroutine
// ids, argument values (pointers, struct words) and pc offsets. What's left
// is the list of functions and file:line positions.
func normalizeStack(stack []byte) []byte {
	lines := strings.Split(strings.TrimRight(string(stack), "\n"), "\n")
	var b bytes.Buffer
	for _, line := range lines {
		switch {
		case stackGoroutineRe.MatchString(line):
			line = "goroutine"
		case strings.HasPrefix(line, "\t"):
			line = stackOffsetRe.ReplaceAllString(line, "")
		case strings.HasPrefix(line, "created by "):
			line = stackCreatedInRe.ReplaceAllString(line, "")
		case strings.HasSuffix(line, ")"):
			// "pkg.(*T).Method(0xc000012345, {0x1, 0x2})": the last '(' opens
			// the argument list; "(*T)" comes earlier.
			if i := strings.LastIndexByte(line, '('); i > 0 {
				line = line[:i] + "(...)"
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// stackSignature is the stack_sha256 field: SHA-256 of the normalized
// stack, so the same panic site gives the same hash every time.
func stackSignature(stack []byte) string {
	sum := sha256.Sum256(normalizeStack(stack))
	return hex.EncodeToString(sum[:])
}

// storePanicDedup is the process-wide deduper dispatchWithPanicGuard uses
// and store.security_stats reports.
var storePanicDedup = newPanicDeduper(panicDedupMaxHashes, panicFullStackInterval)

type panicSeen struct {
	hash      string
	count     uint64
	firstSeen time.Time
	lastSeen  time.Time
	lastFull  time.Time
}

// panicDeduper counts recovered panics per stack signature and decides when
// a full stack may be logged. It remembers at most max signatures (LRU).
type panicDeduper struct {
	mu       sync.Mutex
	max      int
	interval time.Duration
	total    uint64
	evicted  uint64
	byHash   map[string]*list.Element
	lru      *list.List // front = most recently seen; values are *panicSeen
}

func newPanicDeduper(max int, interval time.Duration) *panicDeduper {
	if max < 1 {
		max = 1
	}
	return &panicDeduper{max: max, interval: interval, byHash: make(map[string]*list.Element), lru: list.New()}
}

// observe records one panic. full is true when this line should carry the
// full stack; count is this signature's count; total is all panics.
func (d *panicDeduper) observe(hash string, now time.Time) (full bool, count, total uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.total++
	if el, ok := d.byHash[hash]; ok {
		e := el.Value.(*panicSeen)
		e.count++
		e.lastSeen = now
		d.lru.MoveToFront(el)
		if now.Sub(e.lastFull) >= d.interval {
			e.lastFull = now
			return true, e.count, d.total
		}
		return false, e.count, d.total
	}
	e := &panicSeen{hash: hash, count: 1, firstSeen: now, lastSeen: now, lastFull: now}
	d.byHash[hash] = d.lru.PushFront(e)
	for d.lru.Len() > d.max {
		old := d.lru.Back()
		d.lru.Remove(old)
		delete(d.byHash, old.Value.(*panicSeen).hash)
		d.evicted++
	}
	return true, 1, d.total
}

// panicHashStat is one row of the per-hash counts.
type panicHashStat struct {
	StackSHA256 string `json:"stack_sha256"`
	Count       uint64 `json:"count"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen"`
}

// panicStats is the store.handler_panic block of store.security_stats.
type panicStats struct {
	Total          uint64          `json:"total"`
	TrackedHashes  int             `json:"tracked_hashes"`
	EvictedHashes  uint64          `json:"evicted_hashes"`
	MaxHashes      int             `json:"max_hashes"`
	ByHash         []panicHashStat `json:"by_hash"`
	ByHashTruncate bool            `json:"by_hash_truncated,omitempty"`
}

// snapshot returns the counts, with at most topN signatures (highest count
// first, then most recent).
func (d *panicDeduper) snapshot(topN int) panicStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := panicStats{Total: d.total, TrackedHashes: d.lru.Len(), EvictedHashes: d.evicted, MaxHashes: d.max, ByHash: []panicHashStat{}}
	rows := make([]*panicSeen, 0, d.lru.Len())
	for el := d.lru.Front(); el != nil; el = el.Next() {
		rows = append(rows, el.Value.(*panicSeen))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].count > rows[j].count })
	if len(rows) > topN {
		rows = rows[:topN]
		st.ByHashTruncate = true
	}
	for _, e := range rows {
		st.ByHash = append(st.ByHash, panicHashStat{
			StackSHA256: e.hash,
			Count:       e.count,
			FirstSeen:   e.firstSeen.UTC().Format(time.RFC3339),
			LastSeen:    e.lastSeen.UTC().Format(time.RFC3339),
		})
	}
	return st
}

// storeSecurityStatsCommand is the host-only Store command that reports
// the handler-panic counters to the daemon's security-posture view.
const storeSecurityStatsCommand = "store.security_stats"

// isHostStatsSource reports whether source may read store.security_stats.
// It allows exactly the sources config/acls.yaml grants the command to:
// daemon-internal and daemon-internal-N. The hub sets Source to the
// registered component id, and those ids are reserved for host processes.
func isHostStatsSource(source string) bool {
	return source == "daemon-internal" || (strings.HasPrefix(source, "daemon-internal-") && len(source) > len("daemon-internal-"))
}

// handleStoreSecurityStats answers store.security_stats.
func handleStoreSecurityStats(msg Message, response *Message, d *panicDeduper) {
	if !isHostStatsSource(msg.Source) {
		response.Command = "error"
		response.Payload = "ERR_PERMISSION_DENIED: store.security_stats is host-only"
		return
	}
	response.Command = storeSecurityStatsCommand
	payload := map[string]interface{}{
		storeHandlerPanicEventName: d.snapshot(panicStatsTopN),
	}
	for k, v := range auditStats() {
		payload[k] = v
	}
	response.Payload = payload
}

// truncateUTF8 caps s at max bytes without splitting a rune. Invalid UTF-8
// is left for json.Marshal, which replaces it with U+FFFD.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
