package main

import (
	"bufio"
	"bytes"
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// The Store's audit log is append-only JSON lines in auditLogFile, one entry
// per line, mode 0600. Appends write only the new entries and fsync. A whole
// file is only ever replaced atomically (temp file, fsync, rename): when an
// older audit.json is migrated, after a failed or partial write, or when a
// torn last line is found on load. The in-memory log and computeMerkleRoot
// are unchanged: the root is still the hash of the whole log, so
// audit.get_root and the roots attached to replies mean the same thing.
const (
	auditLogFile       = "audit.jsonl"
	legacyAuditLogFile = "audit.json"

	// auditFieldCap caps each string field of an audit.append entry (url,
	// reason, ...). auditEntryMaxBytes caps the whole entry after that.
	auditFieldCap      = 512
	auditEntryMaxBytes = 4 << 10

	// Identical blocked_request events (same source, skill, url) within
	// auditCoalesceWindow are counted instead of appended. The next one
	// after the window is appended with repeats_suppressed. At most
	// auditCoalesceMaxKeys events are tracked; the least recent goes first.
	auditCoalesceWindow  = time.Minute
	auditCoalesceMaxKeys = 256

	storeAuditAppendFailedEvent = "store.audit_append_failed"
	auditAppendFailedStat       = "audit.append_failed"
	auditCoalescedStat          = "audit.append_coalesced"
)

var (
	auditAppendFailedTotal atomic.Uint64
	auditCoalescedTotal    atomic.Uint64
)

// auditDiskState is what is known about one audit file on disk.
type auditDiskState struct {
	// persisted is how many leading entries of the in-memory log the file
	// holds. Valid only when known is true.
	persisted int
	known     bool
}

var (
	auditDiskMu sync.Mutex
	auditDisk   = map[string]*auditDiskState{}
)

func auditDiskFor(path string) *auditDiskState {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs
	}
	st, ok := auditDisk[key]
	if !ok {
		st = &auditDiskState{}
		auditDisk[key] = st
	}
	return st
}

func marshalAuditLines(entries []interface{}) ([]byte, error) {
	var buf bytes.Buffer
	for i, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("audit entry %d: %w", i, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// writeAuditFileAtomic replaces path with entries as JSON lines: a temp file
// in the same directory, written, fsynced, chmod 0600, then renamed over
// path, and the directory fsynced. A crash leaves the old file or the new
// one, never a mix.
func writeAuditFileAtomic(path string, entries []interface{}) error {
	data, err := marshalAuditLines(entries)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".audit-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// appendAuditLines appends entries to path (O_APPEND, mode 0600) and fsyncs.
func appendAuditLines(path string, entries []interface{}) error {
	data, err := marshalAuditLines(entries)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// saveAuditToFile persists log to path. Entries already on disk aren't
// rewritten: only the new tail is appended. When the file's state isn't
// known (no load or save yet, a failed write, a torn tail, or a log shorter
// than what was persisted), the whole file is replaced atomically. Any
// error is returned and leaves the state unknown, so the next save repairs
// the file.
func saveAuditToFile(path string, log []interface{}) error {
	auditDiskMu.Lock()
	defer auditDiskMu.Unlock()
	st := auditDiskFor(path)
	var err error
	if st.known && st.persisted <= len(log) {
		if st.persisted < len(log) {
			err = appendAuditLines(path, log[st.persisted:])
		}
	} else {
		err = writeAuditFileAtomic(path, log)
	}
	if err != nil {
		st.known = false
		return err
	}
	st.known = true
	st.persisted = len(log)
	return nil
}

// loadAuditFromFile reads an audit file: JSON lines, or a JSON array (the
// older audit.json format). A last line cut off by a crash is dropped; any
// other unparseable line is kept as a marker entry so the root shows it. In
// either case the next save rewrites the file atomically.
func loadAuditFromFile(path string) []interface{} {
	var entries []interface{}
	data, err := os.ReadFile(path)
	auditDiskMu.Lock()
	defer auditDiskMu.Unlock()
	st := auditDiskFor(path)
	if err != nil {
		// Missing file: an empty log, and nothing on disk to keep.
		st.known = errors.Is(err, os.ErrNotExist)
		st.persisted = 0
		return entries
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			log.Printf("store: audit file %s is not valid JSON: %v", path, err)
			entries = []interface{}{map[string]interface{}{"audit_load_error": "unparseable legacy audit file"}}
		}
		st.known = false
		return entries
	}
	clean := true
	r := bufio.NewReader(bytes.NewReader(data))
	for n := 1; ; n++ {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			terminated := line[len(line)-1] == '\n'
			body := bytes.TrimSpace(line)
			if len(body) > 0 {
				var e interface{}
				if err := json.Unmarshal(body, &e); err == nil {
					entries = append(entries, e)
					if !terminated {
						clean = false
					}
				} else if !terminated && rerr == io.EOF {
					log.Printf("store: dropped a torn last line in %s", path)
					clean = false
				} else {
					entries = append(entries, map[string]interface{}{
						"audit_load_error": "unparseable line",
						"line":             n,
						"raw":              truncateUTF8(string(body), auditFieldCap),
					})
					clean = false
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	st.known = clean
	st.persisted = len(entries)
	return entries
}

// loadAuditLog loads the Store's audit log at startup. An older audit.json
// (one JSON array) is migrated to auditLogFile with an atomic write.
func loadAuditLog() []interface{} {
	if _, err := os.Stat(auditLogFile); err == nil {
		return loadAuditFromFile(auditLogFile)
	}
	if _, err := os.Stat(legacyAuditLogFile); err != nil {
		return loadAuditFromFile(auditLogFile)
	}
	entries := loadAuditFromFile(legacyAuditLogFile)
	if err := saveAuditToFile(auditLogFile, entries); err != nil {
		log.Printf("store: migrating %s to %s: %v", legacyAuditLogFile, auditLogFile, err)
	}
	return entries
}

type auditFailureEvent struct {
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Time     string `json:"time"`
	Command  string `json:"command"`
	Source   string `json:"source"`
	Error    string `json:"error"`
	Total    uint64 `json:"failed_total"`
}

// persistAudit saves the log. A failure bumps audit.append_failed (reported
// by store.security_stats) and writes one SECURITY line.
func persistAudit(log []interface{}, msg Message) {
	if err := saveAuditToFile(auditLogFile, log); err != nil {
		noteAuditWriteFailure(securityLogWriter(), msg, err, time.Now())
	}
}

func noteAuditWriteFailure(out io.Writer, msg Message, err error, now time.Time) {
	total := auditAppendFailedTotal.Add(1)
	line, mErr := json.Marshal(auditFailureEvent{
		Event:    storeAuditAppendFailedEvent,
		Severity: "security",
		Time:     now.UTC().Format(time.RFC3339Nano),
		Command:  truncateUTF8(msg.Command, securityFieldCap),
		Source:   truncateUTF8(msg.Source, securityFieldCap),
		Error:    truncateUTF8(err.Error(), securityFieldCap),
		Total:    total,
	})
	if mErr != nil {
		line = []byte(`{"event":"` + storeAuditAppendFailedEvent + `","severity":"security","marshal_error":true}`)
	}
	_, _ = io.WriteString(out, securityEventPrefix+string(line)+"\n")
}

// capAuditPayload bounds what a sender can put in one entry: each string
// field of an object is capped at auditFieldCap (listed in
// truncated_fields), and an entry still over auditEntryMaxBytes is replaced
// by a marker with its size.
func capAuditPayload(p interface{}) interface{} {
	var out interface{}
	switch v := p.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(v))
		var truncated []string
		for k, val := range v {
			if s, ok := val.(string); ok && len(s) > auditFieldCap {
				m[k] = truncateUTF8(s, auditFieldCap)
				truncated = append(truncated, k)
				continue
			}
			m[k] = val
		}
		if len(truncated) > 0 {
			sort.Strings(truncated)
			m["truncated_fields"] = truncated
		}
		out = m
	case string:
		out = truncateUTF8(v, auditFieldCap)
	default:
		out = v
	}
	b, err := json.Marshal(out)
	if err != nil || len(b) > auditEntryMaxBytes {
		marker := map[string]interface{}{"oversized": true, "bytes": len(b)}
		if m, ok := out.(map[string]interface{}); ok {
			if a, ok := m["action"].(string); ok {
				marker["action"] = truncateUTF8(a, 64)
			}
		}
		return marker
	}
	return out
}

type auditCoalesceSlot struct {
	key        string
	window     time.Time
	suppressed uint64
}

// auditCoalescer counts identical blocked_request events per source, skill
// and url inside auditCoalesceWindow. Bounded by auditCoalesceMaxKeys (LRU).
type auditCoalescer struct {
	mu    sync.Mutex
	order *list.List
	slots map[string]*list.Element
}

func newAuditCoalescer() *auditCoalescer {
	return &auditCoalescer{order: list.New(), slots: map[string]*list.Element{}}
}

var storeAuditCoalescer = newAuditCoalescer()

// admit reports whether an event should be appended, and how many identical
// events were suppressed since the last appended one.
func (c *auditCoalescer) admit(key string, now time.Time) (bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.slots[key]; ok {
		s := el.Value.(*auditCoalesceSlot)
		c.order.MoveToFront(el)
		if now.Sub(s.window) < auditCoalesceWindow {
			s.suppressed++
			return false, 0
		}
		n := s.suppressed
		s.window, s.suppressed = now, 0
		return true, n
	}
	c.slots[key] = c.order.PushFront(&auditCoalesceSlot{key: key, window: now})
	for c.order.Len() > auditCoalesceMaxKeys {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.slots, old.Value.(*auditCoalesceSlot).key)
	}
	return true, 0
}

func (c *auditCoalescer) tracked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// auditAppendEntry builds the stored entry for an audit.append: the
// hub-verified source and a Store timestamp around the capped payload. It
// returns false when an identical blocked_request is being coalesced.
func auditAppendEntry(c *auditCoalescer, msg Message, now time.Time) (map[string]interface{}, bool) {
	payload := capAuditPayload(msg.Payload)
	var suppressed uint64
	if m, ok := payload.(map[string]interface{}); ok && m["action"] == "blocked_request" {
		skill, _ := m["skill_id"].(string)
		url, _ := m["url"].(string)
		key, _ := json.Marshal([]string{msg.Source, skill, url})
		admit, n := c.admit(string(key), now)
		if !admit {
			auditCoalescedTotal.Add(1)
			return nil, false
		}
		suppressed = n
	}
	entry := map[string]interface{}{
		"command":     "audit.append",
		"source":      truncateUTF8(msg.Source, securityFieldCap),
		"received_at": now.UTC().Format(time.RFC3339Nano),
		"entry":       payload,
	}
	if suppressed > 0 {
		entry["repeats_suppressed"] = suppressed
	}
	return entry, true
}

// auditStats is the audit block of store.security_stats.
func auditStats() map[string]interface{} {
	return map[string]interface{}{
		auditAppendFailedStat: map[string]interface{}{"total": auditAppendFailedTotal.Load()},
		auditCoalescedStat: map[string]interface{}{
			"total":        auditCoalescedTotal.Load(),
			"tracked_keys": storeAuditCoalescer.tracked(),
			"max_keys":     auditCoalesceMaxKeys,
		},
	}
}
