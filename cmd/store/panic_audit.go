package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime/debug"
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
	Stack       string `json:"stack"`
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
			logRecoveredHandlerPanic(securityLogWriter(), msg, rec, debug.Stack(), time.Now())
			response.Command = "error"
			response.Payload = fmt.Sprintf("internal error handling %s", truncateUTF8(msg.Command, securityFieldCap))
			skipReply = false
		}
	}()
	return storeDispatch(msg, response, w)
}

// logRecoveredHandlerPanic writes one SECURITY line for a recovered panic.
func logRecoveredHandlerPanic(out io.Writer, msg Message, rec interface{}, stack []byte, now time.Time) {
	sum := sha256.Sum256(stack)
	ev := storeSecurityEvent{
		Event:       storeHandlerPanicEventName,
		Severity:    "security",
		Time:        now.UTC().Format(time.RFC3339Nano),
		Command:     truncateUTF8(msg.Command, securityFieldCap),
		Source:      truncateUTF8(msg.Source, securityFieldCap),
		Destination: truncateUTF8(msg.Destination, securityFieldCap),
		Panic:       truncateUTF8(fmt.Sprint(rec), securityPanicCap),
		StackSHA256: hex.EncodeToString(sum[:]),
		Stack:       truncateUTF8(string(stack), securityStackCap),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		// Not reachable with string fields. Still emit a marked line.
		line = []byte(`{"event":"` + storeHandlerPanicEventName + `","severity":"security","marshal_error":true}`)
	}
	_, _ = io.WriteString(out, securityEventPrefix+string(line)+"\n")
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
