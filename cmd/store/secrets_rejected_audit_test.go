package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func withSecretsRejectedAudit(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldWriter := securityLogWriter
	oldCount := secretsUpdateRejected.Load()
	securityLogWriter = func() io.Writer { return &buf }
	secretsUpdateRejected.Store(0)
	t.Cleanup(func() {
		securityLogWriter = oldWriter
		secretsUpdateRejected.Store(oldCount)
	})
	return &buf
}

func parseSecretsRejectedLines(t *testing.T, out string) []map[string]interface{} {
	t.Helper()
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	var evs []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, securityEventPrefix) {
			t.Fatalf("line without %q prefix: %q", securityEventPrefix, line)
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, securityEventPrefix)), &ev); err != nil {
			t.Fatalf("security line is not one JSON object: %v: %q", err, line)
		}
		evs = append(evs, ev)
	}
	return evs
}

func TestDispatchSecretsResponseRejectedAudit(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	buf := withSecretsRejectedAudit(t)

	resp := Message{}
	skip := dispatchStoreCommand(Message{
		Source:      "network-boundary",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"error": "bad signature", "skills": float64(2)},
	}, &resp, w)
	if !skip {
		t.Fatal("secrets.response with error: skip=false, want true")
	}
	if resp.Command != "" {
		t.Fatalf("secrets.response with error: command %q, want empty", resp.Command)
	}
	if got := secretsUpdateRejected.Load(); got != 1 {
		t.Fatalf("counter = %d, want 1", got)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 SECURITY line, got %d: %q", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], securityEventPrefix) {
		t.Fatalf("line %q does not start with %q", lines[0], securityEventPrefix)
	}
	evs := parseSecretsRejectedLines(t, buf.String())
	if len(evs) != 1 {
		t.Fatalf("parsed %d events", len(evs))
	}
	ev := evs[0]
	if ev["event"] != storeSecretsUpdateRejectedEventName {
		t.Fatalf("event = %v, want %s", ev["event"], storeSecretsUpdateRejectedEventName)
	}
	if ev["error"] != "bad signature" {
		t.Fatalf("error = %v, want %q", ev["error"], "bad signature")
	}
}

func TestDispatchSecretsResponseRejectedAuditTruncatesError(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	buf := withSecretsRejectedAudit(t)

	longErr := "x" + strings.Repeat("é", 2499) + "y"
	if len(longErr) != 5000 {
		t.Fatalf("fixture length %d, want 5000", len(longErr))
	}
	resp := Message{}
	skip := dispatchStoreCommand(Message{
		Source:      "network-boundary",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"error": longErr},
	}, &resp, w)
	if !skip {
		t.Fatal("skip=false, want true")
	}
	evs := parseSecretsRejectedLines(t, buf.String())
	if len(evs) != 1 {
		t.Fatalf("want 1 line, got %d: %q", len(evs), buf.String())
	}
	errField, _ := evs[0]["error"].(string)
	if len(errField) > securityFieldCap {
		t.Fatalf("error field length %d, want <= %d", len(errField), securityFieldCap)
	}
	if !utf8.ValidString(errField) {
		t.Fatalf("error field is not valid UTF-8")
	}
}

func TestDispatchSecretsResponseNoErrorNoAudit(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	buf := withSecretsRejectedAudit(t)

	for _, name := range []string{"absent", "empty"} {
		payload := map[string]interface{}{"status": "ok"}
		if name == "empty" {
			payload["error"] = ""
		}
		before := secretsUpdateRejected.Load()
		beforeLen := buf.Len()
		resp := Message{}
		skip := dispatchStoreCommand(Message{
			Source:      "network-boundary",
			Destination: "store",
			Command:     "secrets.response",
			Payload:     payload,
		}, &resp, w)
		if !skip {
			t.Fatalf("%s: skip=false, want true", name)
		}
		if secretsUpdateRejected.Load() != before {
			t.Fatalf("%s: counter changed", name)
		}
		if buf.Len() != beforeLen {
			t.Fatalf("%s: wrote %q", name, buf.String())
		}
	}
}

func TestDispatchSecretsResponseOtherSourceNoAudit(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	buf := withSecretsRejectedAudit(t)

	before := secretsUpdateRejected.Load()
	resp := Message{}
	skip := dispatchStoreCommand(Message{
		Source:      "builder",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"error": "nope"},
	}, &resp, w)
	if skip {
		t.Fatal("skip=true, want false")
	}
	if secretsUpdateRejected.Load() != before {
		t.Fatal("counter changed")
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote %q", buf.String())
	}
}

func TestStoreSecurityStatsIncludesSecretsUpdateRejected(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	buf := withSecretsRejectedAudit(t)

	dispatchStoreCommand(Message{
		Source:      "network-boundary",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"error": "bad blob"},
	}, &Message{}, w)
	if buf.Len() == 0 {
		t.Fatal("expected a rejected-update SECURITY line before stats")
	}

	resp := Message{}
	skip := dispatchStoreCommand(Message{
		Source:      "daemon-internal",
		Destination: "store",
		Command:     storeSecurityStatsCommand,
	}, &resp, w)
	if skip {
		t.Fatal("skipReply")
	}
	b, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	block := got[storeSecretsUpdateRejectedEventName]
	total, _ := block["total"].(float64)
	if uint64(total) != secretsUpdateRejected.Load() {
		t.Fatalf("stats total = %v, counter = %d", block["total"], secretsUpdateRejected.Load())
	}
}
