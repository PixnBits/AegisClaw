package main

import (
	"crypto/ed25519"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
)

// buildLLMUsageRecord returns the llm.usage.record payload.
// On success, model and token fields are copied from usage. duration_ms is
// copied only when usage includes it; otherwise the wall-clock value set
// above is kept. On failure, token counts are zero, duration_ms is wall time
// since started, and a non-empty errMsg is stored under "error" truncated to
// 200 runes (a byte cut would split a multibyte character).
func buildLLMUsageRecord(agentID, model string, usage map[string]interface{}, success bool, errMsg string, started time.Time) map[string]interface{} {
	rec := map[string]interface{}{
		"agent_id":          agentID,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"model":             model,
		"tokens_prompt":     0,
		"tokens_completion": 0,
		"duration_ms":       int(time.Since(started).Milliseconds()),
		"success":           success,
	}
	if success {
		if usage != nil {
			rec["model"] = usage["model"]
			rec["tokens_prompt"] = usage["prompt_tokens"]
			rec["tokens_completion"] = usage["completion_tokens"]
			// Missing total_duration must not wipe the wall-clock duration set above.
			if d, ok := usage["duration_ms"]; ok && d != nil {
				rec["duration_ms"] = d
			}
		}
		return rec
	}
	if errMsg != "" {
		r := []rune(errMsg)
		if len(r) > 200 {
			errMsg = string(r[:200])
		}
		rec["error"] = errMsg
	}
	return rec
}

// parseOllamaForLLMCall extracts the generated text and usage metrics (tokens, duration) from
// the raw JSON returned by Ollama /api/generate. This is the central point for accurate
// per-call LLM usage collection (outside any Agent Runtime guest VM). Unit tested.
func parseOllamaForLLMCall(raw, model string) (string, map[string]interface{}) {
	text := raw
	usage := map[string]interface{}{"model": model}
	var ollama map[string]interface{}
	if json.Unmarshal([]byte(raw), &ollama) == nil {
		if r, ok := ollama["response"].(string); ok && r != "" {
			text = r
		}
		if v, ok := ollama["prompt_eval_count"].(float64); ok {
			usage["prompt_tokens"] = int(v)
		}
		if v, ok := ollama["eval_count"].(float64); ok {
			usage["completion_tokens"] = int(v)
		}
		if v, ok := ollama["total_duration"].(float64); ok {
			usage["duration_ms"] = int(v / 1e6) // ns -> ms
		}
		if m, ok := ollama["model"].(string); ok && m != "" {
			usage["model"] = m
		}
		usage["success"] = true
	} else {
		usage["success"] = true
	}
	return text, usage
}

// boundaryShouldAnswer reports whether the hub read loop should encode a reply.
// Non-request frames must not be answered: the hub replies to an error at once,
// and {"error":"ERR_ACL_VIOLATION"} decodes with an empty command, so an error
// reply ping-pongs. Reply commands follow aegishub isOneWayHubReply (response,
// ack, *.response, and the known store/memory replies). error and "" are not in
// that helper, but this loop still must not answer them. Genuine requests
// (llm.call, network.request, secrets.*, version, and unknown commands) return true.
func boundaryShouldAnswer(command string) bool {
	switch command {
	case "llm.usage.recorded":
		// Reply to our best-effort usage emit (older Stores). Another frame
		// would be a new hub RPC on this connection.
		return false
	case "error", "", "response", "ack":
		return false
	case "channel.posted", "channel.data", "channel.created", "channel.joined",
		"channel.archived", "channel.list", "channel.member_added",
		"memory.context", "memory.response":
		return false
	}
	if strings.HasSuffix(command, ".response") {
		return false
	}
	return true
}

// emitLLMUsageRecord sends llm.usage.record to Store. Best-effort: encode errors
// are logged and swallowed, and the caller does not wait for a Store reply.
// mu is connMutex so the frame cannot interleave with other writes on the hub connection.
func emitLLMUsageRecord(enc *json.Encoder, mu *sync.Mutex, priv ed25519.PrivateKey, rec map[string]interface{}) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	emitLLMUsageRecordLocked(enc, priv, rec)
}

// emitLLMUsageRecordLocked writes one usage frame. Caller holds mu when the
// encoder is shared with other hub writes.
func emitLLMUsageRecordLocked(enc *json.Encoder, priv ed25519.PrivateKey, rec map[string]interface{}) {
	if enc == nil || rec == nil {
		return
	}
	recMsg := Message{
		Source:      "network-boundary",
		Destination: "store",
		Command:     "llm.usage.record",
		Payload:     rec,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	signMessage(&recMsg, priv)
	if err := enc.Encode(recMsg); err != nil {
		log.Printf("llm.usage.record emit failed: %v", err)
		return
	}
	markUsagePushOutstanding()
}

// usageDropFlushTick is how often a quiet read loop rechecks the drop window.
// The decision uses usageDropNow, not the ticker's clock, so tests can move
// the window without waiting.
const usageDropFlushTick = time.Second

// usageDropLogEvery bounds drop logs so a down Store cannot flood stderr.
const usageDropLogEvery = time.Minute

// usageDropNow is the clock for the drop log window. Tests replace it.
var usageDropNow = time.Now

var (
	usageDropMu          sync.Mutex
	usagePushOutstanding int
	usageDropPending     int
	usageDropLast        time.Time
)

func markUsagePushOutstanding() {
	now := usageDropNow()
	usageDropMu.Lock()
	usagePushOutstanding++
	// An emit is not a drop. It still flushes a count whose window has elapsed
	// so the last burst is not stuck waiting for another error.
	flushUsageDropsLocked(now, false)
	usageDropMu.Unlock()
}

func usagePushOutstandingCount() int {
	usageDropMu.Lock()
	defer usageDropMu.Unlock()
	return usagePushOutstanding
}

func resetUsageDropState() {
	usageDropMu.Lock()
	usagePushOutstanding = 0
	usageDropPending = 0
	usageDropLast = time.Time{}
	usageDropMu.Unlock()
}

// noteUsagePushHubError accounts for one hub frame against in-flight
// llm.usage.record pushes.
//
// The hub does acknowledge a one-way push. forwardHubRPC (cmd/aegishub)
// encodes the push to the destination, then writes a reply to the sender:
// command "response", payload {"status":"accepted"}. It does not stay silent.
// Command "ack" / {"status":"delivered"} is a different path
// (forwardReplyToRequester after deliverPendingRPC), not the push ack.
// Both commands are acknowledgements, not usage errors: each retires one
// outstanding push and the counter floors at 0. An encode failure is command
// "error" and stays a drop. llm.usage.recorded is not an ack (the ACL denies
// it; older Stores may still emit it).
//
// Error frames do not name the original command, so each successful emit
// increments a counter and the next hub error (command "error", or an empty
// command such as {"error":"ERR_*"}) attributes one outstanding push. At most
// one line is written per usageDropLogEvery; the line's count is every drop
// since the previous line. Drops still inside the window sit in
// usageDropPending and are flushed once the window has elapsed even when no
// further error arrives (any later frame, a later emit, the read-loop ticker,
// or shutdown).
func noteUsagePushHubError(msg Message) {
	now := usageDropNow()
	usageDropMu.Lock()
	defer usageDropMu.Unlock()
	if isUsageHubAck(msg) {
		if usagePushOutstanding > 0 {
			usagePushOutstanding--
		}
		flushUsageDropsLocked(now, false)
		return
	}
	if msg.Command != "error" && msg.Command != "" {
		flushUsageDropsLocked(now, false)
		return
	}
	if usagePushOutstanding == 0 {
		flushUsageDropsLocked(now, false)
		return
	}
	usagePushOutstanding--
	usageDropPending++
	if !usageDropLast.IsZero() && now.Sub(usageDropLast) < usageDropLogEvery {
		return
	}
	log.Printf("llm.usage.record dropped: hub error after usage push (count=%d)", usageDropPending)
	usageDropPending = 0
	usageDropLast = now
}

// isUsageHubAck reports the hub frames that mean "push accepted", not "push failed".
// See noteUsagePushHubError for which command the hub actually sends.
func isUsageHubAck(msg Message) bool {
	return msg.Command == "response" || msg.Command == "ack"
}

// flushUsageDropPending logs a suppressed drop count once usageDropLogEvery
// has elapsed. It does not log inside the window and does not log when nothing
// is pending. Tests move the window with usageDropNow.
func flushUsageDropPending() {
	flushUsageDrops(false)
}

// flushUsageDropsOnShutdown logs any pending drops even inside the window.
// The read loop calls it when the hub connection ends, so a quiet exit still
// reports the last burst.
func flushUsageDropsOnShutdown() {
	flushUsageDrops(true)
}

func flushUsageDrops(force bool) {
	now := usageDropNow()
	usageDropMu.Lock()
	flushUsageDropsLocked(now, force)
	usageDropMu.Unlock()
}

func flushUsageDropsLocked(now time.Time, force bool) {
	if usageDropPending == 0 {
		return
	}
	if !force && !usageDropLast.IsZero() && now.Sub(usageDropLast) < usageDropLogEvery {
		return
	}
	log.Printf("llm.usage.record dropped: hub error after usage push (count=%d)", usageDropPending)
	usageDropPending = 0
	usageDropLast = now
}

// startUsageDropFlusher ticks while the hub read loop is idle. Stop flushes
// whatever is still pending. The returned func is safe to call once.
func startUsageDropFlusher() func() {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(usageDropFlushTick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				flushUsageDropsOnShutdown()
				return
			case <-ticker.C:
				flushUsageDropPending()
			}
		}
	}()
	return func() {
		close(stop)
		wg.Wait()
	}
}

// serveBoundaryFrame is one iteration of the hub read loop after a successful
// decode. The boundaryShouldAnswer call site lives here: non-requests are not
// written, and dispatch runs only for real requests. The loop and the tests
// both call this function.
func serveBoundaryFrame(msg Message, enc *json.Encoder, mu *sync.Mutex, priv ed25519.PrivateKey, dispatch func(Message) (Message, map[string]interface{})) {
	if !boundaryShouldAnswer(msg.Command) {
		noteUsagePushHubError(msg)
		return
	}
	// Requests are not usage errors. They still flush a drop count whose
	// window has already elapsed, so the read loop does not need another error.
	flushUsageDropPending()
	response, pending := dispatch(msg)
	// Guest response is written before usage. A usage encode error is swallowed
	// and does not change or fail the response.
	if err := encodeResponseAndMaybeUsage(enc, mu, priv, &response, pending); err != nil {
		log.Println("Failed to send response:", err)
	}
}

// encodeResponseAndMaybeUsage writes the guest response first, then (if rec is
// non-nil) best-effort llm.usage.record. The usage write cannot precede the
// response, and a usage encode error is not returned.
func encodeResponseAndMaybeUsage(enc *json.Encoder, mu *sync.Mutex, priv ed25519.PrivateKey, response *Message, rec map[string]interface{}) error {
	signMessage(response, priv)
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	err := enc.Encode(response)
	if rec != nil {
		emitLLMUsageRecordLocked(enc, priv, rec)
	}
	return err
}
