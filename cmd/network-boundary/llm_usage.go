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
