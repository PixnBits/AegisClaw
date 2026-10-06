package main

import (
	"encoding/json"
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
)

func resetLLMUsageRecords() {
	llmUsageMu.Lock()
	llmUsageRecords = nil
	llmUsageMu.Unlock()
}

func llmUsageSnapshot() []map[string]interface{} {
	llmUsageMu.Lock()
	defer llmUsageMu.Unlock()
	out := make([]map[string]interface{}, len(llmUsageRecords))
	copy(out, llmUsageRecords)
	return out
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
// an allowlisted, capped copy of the payload. Anything else is ignored.
func handleLLMUsageRecord(msg Message) Message {
	if msg.Source != "network-boundary" {
		return Message{Command: "error", Payload: "llm.usage.record rejected"}
	}
	raw, ok := msg.Payload.(map[string]interface{})
	if !ok || raw == nil {
		return Message{Command: "error", Payload: "invalid usage record"}
	}
	rec := sanitizeLLMUsageRecord(raw)
	llmUsageMu.Lock()
	llmUsageRecords = append(llmUsageRecords, rec)
	if len(llmUsageRecords) > llmUsageMaxRecords {
		trimmed := make([]map[string]interface{}, llmUsageTrimTo)
		copy(trimmed, llmUsageRecords[len(llmUsageRecords)-llmUsageTrimTo:])
		llmUsageRecords = trimmed
	}
	llmUsageMu.Unlock()
	return Message{Command: "llm.usage.recorded", Payload: map[string]interface{}{"ok": true}}
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

func handleLLMUsageRecent(msg Message) Message {
	limit := llmUsageRecentDefault
	if p, ok := msg.Payload.(map[string]interface{}); ok {
		if _, present := p["limit"]; present {
			limit = usageRecentLimit(p["limit"])
		}
	}
	records := llmUsageSnapshot()
	n := len(records)
	start := 0
	if n > limit {
		start = n - limit
	}
	out := append([]map[string]interface{}(nil), records[start:]...)
	return Message{Command: "llm.usage.recent", Payload: out}
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
	if s, ok := cappedUsageString(in["timestamp"]); ok && s != "" {
		out["timestamp"] = s
	} else {
		out["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	}
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

func nonNegUsageNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		if n < 0 {
			return 0, false
		}
		return n, true
	case int32:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case int64:
		if n < 0 || n > int64(^uint(0)>>1) {
			return 0, false
		}
		return int(n), true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > float64(int(^uint(0)>>1)) {
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
