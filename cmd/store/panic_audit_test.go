package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func withPanicGuardSeams(t *testing.T, dispatch func(Message, *Message, *storeWorld) bool) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldDispatch, oldWriter := storeDispatch, securityLogWriter
	storeDispatch = dispatch
	securityLogWriter = func() io.Writer { return &buf }
	t.Cleanup(func() { storeDispatch, securityLogWriter = oldDispatch, oldWriter })
	return &buf
}

func securityLines(t *testing.T, out string) []storeSecurityEvent {
	t.Helper()
	var evs []storeSecurityEvent
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, securityEventPrefix) {
			t.Fatalf("line without %q prefix: %q", securityEventPrefix, line)
		}
		var ev storeSecurityEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, securityEventPrefix)), &ev); err != nil {
			t.Fatalf("security line is not one JSON object: %v: %q", err, line)
		}
		evs = append(evs, ev)
	}
	return evs
}

func TestDispatchWithPanicGuardLogsSecurityEvent(t *testing.T) {
	buf := withPanicGuardSeams(t, func(Message, *Message, *storeWorld) bool {
		panic("interface conversion: interface {} is string, not map[string]interface {}")
	})
	// The command is attacker-controlled: a newline must not forge a second event.
	msg := Message{Source: "agent-x", Destination: "store", Command: "channel.create\nSECURITY {\"event\":\"forged\"}"}
	resp := Message{Command: "channel.created"}
	skip := dispatchWithPanicGuard(msg, &resp, &storeWorld{})
	if skip {
		t.Fatal("panic path must still reply (skipReply=false)")
	}
	if resp.Command != "error" {
		t.Fatalf("reply command = %q, want error", resp.Command)
	}
	if p, _ := resp.Payload.(string); strings.Contains(p, "interface conversion") {
		t.Fatalf("panic text leaked into reply: %q", p)
	}
	evs := securityLines(t, buf.String())
	if len(evs) != 1 {
		t.Fatalf("want exactly 1 SECURITY line, got %d: %q", len(evs), buf.String())
	}
	ev := evs[0]
	if ev.Event != storeHandlerPanicEventName || ev.Severity != "security" {
		t.Fatalf("event/severity = %q/%q", ev.Event, ev.Severity)
	}
	if ev.Command != msg.Command || ev.Source != "agent-x" || ev.Destination != "store" {
		t.Fatalf("message fields not recorded: %+v", ev)
	}
	if !strings.Contains(ev.Panic, "interface conversion") {
		t.Fatalf("panic value missing: %q", ev.Panic)
	}
	if ev.Stack == "" || len(ev.StackSHA256) != 64 {
		t.Fatalf("stack/stack_sha256 missing: %d bytes, sha %q", len(ev.Stack), ev.StackSHA256)
	}
	if _, err := time.Parse(time.RFC3339Nano, ev.Time); err != nil {
		t.Fatalf("time %q: %v", ev.Time, err)
	}
}

func TestDispatchWithPanicGuardNoPanicNoSecurityEvent(t *testing.T) {
	buf := withPanicGuardSeams(t, func(_ Message, r *Message, _ *storeWorld) bool {
		r.Command = "error"
		r.Payload = "invalid payload"
		return false
	})
	resp := Message{}
	dispatchWithPanicGuard(Message{Command: "channel.create"}, &resp, &storeWorld{})
	if buf.Len() != 0 {
		t.Fatalf("ordinary error reply must not emit a SECURITY event: %q", buf.String())
	}
	if resp.Payload != "invalid payload" {
		t.Fatalf("dispatcher reply overwritten: %+v", resp)
	}
}

func TestDispatchWithPanicGuardPassesSkipReply(t *testing.T) {
	withPanicGuardSeams(t, func(Message, *Message, *storeWorld) bool { return true })
	if !dispatchWithPanicGuard(Message{Command: "llm.usage.record"}, &Message{}, &storeWorld{}) {
		t.Fatal("skipReply from the dispatcher must pass through")
	}
}

func TestLogRecoveredHandlerPanicCapsFields(t *testing.T) {
	var buf bytes.Buffer
	// One ASCII byte, then 2-byte runes: every even cap lands mid-rune.
	long := "x" + strings.Repeat("é", 4096)
	logRecoveredHandlerPanic(&buf, Message{Command: long, Source: long}, long, []byte(strings.Repeat("s", 64<<10)), time.Now())
	evs := securityLines(t, buf.String())
	if len(evs) != 1 {
		t.Fatalf("got %d lines", len(evs))
	}
	ev := evs[0]
	if len(ev.Command) > securityFieldCap || len(ev.Source) > securityFieldCap || len(ev.Panic) > securityPanicCap || len(ev.Stack) > securityStackCap {
		t.Fatalf("caps not applied: cmd=%d src=%d panic=%d stack=%d", len(ev.Command), len(ev.Source), len(ev.Panic), len(ev.Stack))
	}
	for name, v := range map[string]string{"command": ev.Command, "source": ev.Source, "panic": ev.Panic} {
		if !utf8.ValidString(v) || strings.ContainsRune(v, utf8.RuneError) {
			t.Fatalf("%s split a rune: %q", name, v[len(v)-4:])
		}
	}
}
