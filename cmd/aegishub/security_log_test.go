package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *lockedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func withHubSecurityLog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	old := hubSecurityLogWriter
	hubSecurityLogWriter = func() io.Writer { return buf }
	t.Cleanup(func() { hubSecurityLogWriter = old })
	return buf
}

func withACLRules(t *testing.T, rules []ACLRule) {
	t.Helper()
	orig := aclRules
	aclRules = rules
	t.Cleanup(func() { aclRules = orig })
}

func parseHubSecurityLines(t *testing.T, out string) []map[string]interface{} {
	t.Helper()
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	var evs []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, hubSecurityEventPrefix) {
			t.Fatalf("line without %q prefix: %q", hubSecurityEventPrefix, line)
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, hubSecurityEventPrefix)), &ev); err != nil {
			t.Fatalf("security line is not one JSON object: %v: %q", err, line)
		}
		evs = append(evs, ev)
	}
	return evs
}

func TestForwardStoreSecretsUpdateRefusedAudit(t *testing.T) {
	buf := withHubSecurityLog(t)
	withACLRules(t, []ACLRule{{
		Source:      "store",
		Destination: "network-boundary",
		Commands:    []string{"secrets.push"},
	}})

	ok := forwardStoreSecretsUpdate(Message{
		Source:      "store",
		Destination: "network-boundary",
		Command:     "secrets.update",
		Payload:     map[string]interface{}{"ciphertext": "blob-1"},
	})
	if ok {
		t.Fatal("forwardStoreSecretsUpdate = true without an ACL grant")
	}
	raw := buf.String()
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if raw == "" || len(lines) != 1 {
		t.Fatalf("want exactly 1 SECURITY line, got %d: %q", len(lines), raw)
	}
	if !strings.HasPrefix(lines[0], hubSecurityEventPrefix) {
		t.Fatalf("line %q does not start with %q", lines[0], hubSecurityEventPrefix)
	}
	evs := parseHubSecurityLines(t, raw)
	if len(evs) != 1 {
		t.Fatalf("parsed %d events", len(evs))
	}
	ev := evs[0]
	if ev["event"] != hubSecretsUpdateRefusedEventName {
		t.Fatalf("event = %v, want %s", ev["event"], hubSecretsUpdateRefusedEventName)
	}
	if ev["source"] != "store" || ev["destination"] != "network-boundary" || ev["command"] != "secrets.update" {
		t.Fatalf("fields %+v", ev)
	}
}

func TestForwardStoreSecretsUpdateRefusedAuditTruncatesFields(t *testing.T) {
	buf := withHubSecurityLog(t)
	withACLRules(t, []ACLRule{{
		Source:      "store",
		Destination: "network-boundary",
		Commands:    []string{"secrets.push"},
	}})

	longSrc := "x" + strings.Repeat("é", 2499) + "y"
	if len(longSrc) != 5000 {
		t.Fatalf("fixture length %d, want 5000", len(longSrc))
	}
	ok := forwardStoreSecretsUpdate(Message{
		Source:      longSrc,
		Destination: "network-boundary",
		Command:     "secrets.update",
	})
	if ok {
		t.Fatal("forwardStoreSecretsUpdate = true, want ACL deny")
	}
	evs := parseHubSecurityLines(t, buf.String())
	if len(evs) != 1 {
		t.Fatalf("want 1 line, got %d: %q", len(evs), buf.String())
	}
	for k, v := range evs[0] {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if len(s) > hubSecurityFieldCap {
			t.Fatalf("%s length %d, want <= %d", k, len(s), hubSecurityFieldCap)
		}
		if !utf8.ValidString(s) {
			t.Fatalf("%s is not valid UTF-8", k)
		}
	}
}

func TestForwardStoreSecretsUpdateGrantedNoAudit(t *testing.T) {
	buf := withHubSecurityLog(t)
	withACLRules(t, []ACLRule{{
		Source:      "store",
		Destination: "network-boundary",
		Commands:    []string{"secrets.update"},
	}})

	ok := forwardStoreSecretsUpdate(Message{
		Source:      "store",
		Destination: "network-boundary",
		Command:     "secrets.update",
		Payload:     map[string]interface{}{"ciphertext": "blob-1"},
	})
	if !ok {
		t.Fatal("forwardStoreSecretsUpdate = false with an ACL grant")
	}
	if buf.String() != "" {
		t.Fatalf("SECURITY line on grant: %q", buf.String())
	}
}
