package main

import (
	"encoding/json"
	"log"
	"math"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	llmUsageMaxRecords    = 10000
	llmUsageTrimTo        = 5000
	llmUsageRecentDefault = 50
	llmUsageRecentMax     = 500
	llmUsageMaxString     = 256
	// llmUsageMaxNumeric is the per-field cap for token and duration counts.
	// Values above it are dropped, not clamped. float64(MaxInt64) rounds to
	// 2^63, so a comparison against that bound lets 2^63 through and int(n)
	// becomes negative. With at most llmUsageMaxRecords+1 stored rows (the
	// slice is trimmed after the append that exceeds the cap) and two summed
	// fields, the largest aggregate is 10001 * 2 * 1e12 = 2.0002e16, which
	// fits in int64 (max ~9.22e18). Summary sums cannot overflow.
	llmUsageMaxNumeric = 1e12
	// llmUsageFutureSkew is how far ahead of receive time a record timestamp
	// may be. Further ahead, or unparseable, is replaced with receive time.
	llmUsageFutureSkew = 5 * time.Minute
)

// llmUsageRecords is the LLM usage log. It is in memory only and is lost on
// Store restart (not written to disk).
//
// runStore decodes and handles one hub message at a time, so production
// traffic is not concurrent. llmUsageMu still guards this package-level slice
// so the cap trim cannot race a query or a test.
var (
	llmUsageMu      sync.Mutex
	llmUsageRecords []map[string]interface{}
	// llmUsageSeq is the last seq assigned. The first accepted record is 1.
	// Trimming the log does not rewind it.
	llmUsageSeq uint64
)

func resetLLMUsageRecords() {
	llmUsageMu.Lock()
	defer llmUsageMu.Unlock()
	llmUsageRecords = nil
	llmUsageSeq = 0
}

func llmUsageSnapshot() []map[string]interface{} {
	recs, _ := llmUsageState()
	return recs
}

func llmUsageState() ([]map[string]interface{}, uint64) {
	llmUsageMu.Lock()
	defer llmUsageMu.Unlock()
	out := make([]map[string]interface{}, len(llmUsageRecords))
	copy(out, llmUsageRecords)
	return out, llmUsageSeq
}

// computeLLMUsageSummary builds the required aggregates (grand total, windows, by model).
// Also includes by_agent breakdown for per-agent views on individual agent pages and lists.
// Windows approximated by timestamp parsing (recent records kept hot in llmUsageRecords).
func computeLLMUsageSummary(records []map[string]interface{}) map[string]interface{} {
	now := time.Now().UTC()
	grand := map[string]interface{}{"calls": 0, "tokens_prompt": 0, "tokens_completion": 0, "by_model": map[string]interface{}{}}
	lastHour := map[string]interface{}{"calls": 0, "tokens_prompt": 0, "tokens_completion": 0}
	today := map[string]interface{}{"calls": 0, "tokens_prompt": 0, "tokens_completion": 0}
	mtd := map[string]interface{}{"calls": 0, "tokens_prompt": 0, "tokens_completion": 0}
	modelBreak := map[string]int{}
	byAgent := map[string]map[string]interface{}{}

	// p and c are already capped at llmUsageMaxNumeric. See that constant.
	add := func(target map[string]interface{}, p, c int) {
		target["calls"] = target["calls"].(int) + 1
		target["tokens_prompt"] = target["tokens_prompt"].(int) + p
		target["tokens_completion"] = target["tokens_completion"].(int) + c
	}

	for _, r := range records {
		p := usageInt(r["tokens_prompt"])
		c := usageInt(r["tokens_completion"])
		mdl := "unknown"
		if s, ok := r["model"].(string); ok && s != "" {
			mdl = s
		}
		modelBreak[mdl] += (p + c)

		agentID := "unknown"
		if a, ok := r["agent_id"].(string); ok && a != "" {
			agentID = a
		}

		if _, exists := byAgent[agentID]; !exists {
			byAgent[agentID] = map[string]interface{}{
				"calls": 0, "tokens_prompt": 0, "tokens_completion": 0,
				"by_model": map[string]interface{}{},
			}
		}
		agentGrand := byAgent[agentID]
		agentModelBreak := agentGrand["by_model"].(map[string]interface{})
		if _, ok := agentModelBreak[mdl]; !ok {
			agentModelBreak[mdl] = 0
		}
		agentModelBreak[mdl] = agentModelBreak[mdl].(int) + (p + c)
		add(agentGrand, p, c)
		agentGrand["tokens_total"] = agentGrand["tokens_prompt"].(int) + agentGrand["tokens_completion"].(int)
		agentGrand["by_model"] = agentModelBreak

		tsStr, _ := r["timestamp"].(string)
		t, _ := time.Parse(time.RFC3339, tsStr)
		if t.IsZero() {
			t = now
		}
		add(grand, p, c)
		if now.Sub(t) <= time.Hour {
			add(lastHour, p, c)
		}
		if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
			add(today, p, c)
		}
		if t.Year() == now.Year() && t.Month() == now.Month() {
			add(mtd, p, c)
		}
	}

	mb := map[string]interface{}{}
	for k, v := range modelBreak {
		mb[k] = v
	}
	grand["by_model"] = mb
	grand["tokens_total"] = grand["tokens_prompt"].(int) + grand["tokens_completion"].(int)

	byAgentOut := map[string]interface{}{}
	for k, v := range byAgent {
		byAgentOut[k] = v
	}

	return map[string]interface{}{
		"grand":        grand,
		"last_hour":    lastHour,
		"today":        today,
		"mtd":          mtd,
		"models":       mb,
		"record_count": len(records),
		"by_agent":     byAgentOut,
	}
}

func usageInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// handleLLMUsageRecord accepts an append only from network-boundary and stores
// an allowlisted, capped copy of the payload. The return is always a no-reply
// (empty command and nil payload): llm.usage.record is a one-way hub push, so
// neither an accept nor a rejection is encoded. Rejections are logged and not
// stored. Callers must not encode the returned Message.
func handleLLMUsageRecord(msg Message) Message {
	if msg.Source != "network-boundary" {
		log.Printf("llm.usage.record rejected: source %q", msg.Source)
		return Message{}
	}
	raw, ok := msg.Payload.(map[string]interface{})
	if !ok || raw == nil {
		log.Printf("llm.usage.record rejected: invalid payload")
		return Message{}
	}
	appendLLMUsageRecord(sanitizeLLMUsageRecord(raw))
	return Message{}
}

// appendLLMUsageRecord assigns the next seq and appends rec, trimming the log
// at the cap. Unlock is deferred, as everywhere llmUsageMu is taken, so a
// panic recovered by dispatchWithPanicGuard can't leave the mutex held and
// wedge every later llm.usage.* command.
func appendLLMUsageRecord(rec map[string]interface{}) {
	llmUsageMu.Lock()
	defer llmUsageMu.Unlock()
	llmUsageSeq++
	rec["seq"] = llmUsageSeq
	llmUsageRecords = append(llmUsageRecords, rec)
	if len(llmUsageRecords) > llmUsageMaxRecords {
		trimmed := make([]map[string]interface{}, llmUsageTrimTo)
		copy(trimmed, llmUsageRecords[len(llmUsageRecords)-llmUsageTrimTo:])
		llmUsageRecords = trimmed
	}
}

func handleLLMUsageSummary(msg Message) Message {
	filtered := llmUsageSnapshot()
	if p, ok := msg.Payload.(map[string]interface{}); ok {
		if aid, ok := p["agent_id"].(string); ok && aid != "" {
			matched := make([]map[string]interface{}, 0)
			for _, rec := range filtered {
				if a, ok := rec["agent_id"].(string); ok && a == aid {
					matched = append(matched, rec)
				}
			}
			filtered = matched
		}
	}
	return Message{Command: "llm.usage.summary", Payload: computeLLMUsageSummary(filtered)}
}

// handleLLMUsageRecent returns {"records", "last_seq"}. last_seq is the highest
// seq assigned, even when the page is empty, and is not rewound by trim.
// agent_id is applied before limit. Without after_seq, records are the newest
// limit rows. With after_seq, records are the oldest limit rows whose seq is
// greater than after_seq, so a caller can walk forward. A present but
// unparseable after_seq returns an empty page rather than the newest window.
func handleLLMUsageRecent(msg Message) Message {
	limit := llmUsageRecentDefault
	agentID := ""
	afterSeq := uint64(0)
	haveAfter := false
	afterInvalid := false
	if p, ok := msg.Payload.(map[string]interface{}); ok {
		if _, present := p["limit"]; present {
			limit = usageRecentLimit(p["limit"])
		}
		if aid, ok := p["agent_id"].(string); ok {
			agentID = aid
		}
		if _, present := p["after_seq"]; present {
			seq, ok := usageSeqValue(p["after_seq"])
			if !ok {
				afterInvalid = true
			} else {
				haveAfter = true
				afterSeq = seq
			}
		}
	}
	records, lastSeq := llmUsageState()
	if agentID != "" {
		matched := make([]map[string]interface{}, 0)
		for _, rec := range records {
			if a, ok := rec["agent_id"].(string); ok && a == agentID {
				matched = append(matched, rec)
			}
		}
		records = matched
	}
	var out []map[string]interface{}
	switch {
	case afterInvalid:
		out = []map[string]interface{}{}
	case haveAfter:
		newer := make([]map[string]interface{}, 0)
		for _, rec := range records {
			seq, ok := usageSeqValue(rec["seq"])
			if ok && seq > afterSeq {
				newer = append(newer, rec)
			}
		}
		if len(newer) > limit {
			newer = newer[:limit]
		}
		out = newer
	default:
		n := len(records)
		start := 0
		if n > limit {
			start = n - limit
		}
		out = append([]map[string]interface{}(nil), records[start:]...)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return Message{Command: "llm.usage.recent", Payload: map[string]interface{}{
		"records":  out,
		"last_seq": lastSeq,
	}}
}

func usageRecentLimit(v interface{}) int {
	n, ok := nonNegUsageNumber(v)
	if !ok || n <= 0 {
		return llmUsageRecentDefault
	}
	if n > llmUsageRecentMax {
		return llmUsageRecentMax
	}
	return n
}

func sanitizeLLMUsageRecord(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if s, ok := cappedUsageString(in["agent_id"]); ok && s != "" {
		out["agent_id"] = s
	}
	if s, ok := cappedUsageString(in["model"]); ok && s != "" {
		out["model"] = s
	}
	// Unparseable timestamps, and timestamps more than llmUsageFutureSkew in
	// the future, are replaced with receive time. The record is not dropped.
	out["timestamp"] = normalizeUsageTimestamp(in["timestamp"], time.Now())
	if s, ok := cappedUsageString(in["error"]); ok && s != "" {
		out["error"] = s
	}
	for _, key := range []string{"tokens_prompt", "tokens_completion", "duration_ms"} {
		if n, ok := nonNegUsageNumber(in[key]); ok {
			out[key] = n
		}
	}
	if b, ok := in["success"].(bool); ok {
		out["success"] = b
	}
	return out
}

func cappedUsageString(v interface{}) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	if len(s) <= llmUsageMaxString {
		return s, true
	}
	b := []byte(s[:llmUsageMaxString])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b), true
}

// normalizeUsageTimestamp keeps a parseable RFC3339 timestamp that is not more
// than llmUsageFutureSkew ahead of now. Anything else is the receive time, so
// windowed aggregates stay inside real time. The record itself is not dropped.
func normalizeUsageTimestamp(v interface{}, now time.Time) string {
	now = now.UTC()
	fallback := now.Format(time.RFC3339)
	s, ok := cappedUsageString(v)
	if !ok || s == "" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || t.After(now.Add(llmUsageFutureSkew)) {
		return fallback
	}
	return s
}

func usageSeqValue(v interface{}) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case uint:
		return uint64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int32:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, false
		}
		return uint64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return usageSeqValue(f)
	default:
		return 0, false
	}
}

func usageNumberTooLarge(n float64) bool {
	return n > llmUsageMaxNumeric
}

func nonNegUsageNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		if n < 0 || usageNumberTooLarge(float64(n)) {
			return 0, false
		}
		return n, true
	case int32:
		if n < 0 || usageNumberTooLarge(float64(n)) {
			return 0, false
		}
		return int(n), true
	case int64:
		if n < 0 || usageNumberTooLarge(float64(n)) {
			return 0, false
		}
		return int(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || usageNumberTooLarge(n) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return nonNegUsageNumber(f)
	default:
		return 0, false
	}
}
