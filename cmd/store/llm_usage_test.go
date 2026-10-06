package main

import (
	"strings"
	"testing"
	"time"
)

func TestLLMUsageRecordSummaryHandlersLogic(t *testing.T) {
	records := []map[string]interface{}{
		{"agent_id": "coder-1", "model": "qwen", "tokens_prompt": 100, "tokens_completion": 50, "duration_ms": 800, "timestamp": time.Now().Add(-2 * time.Hour).Format(time.RFC3339), "success": true},
		{"agent_id": "pm", "model": "qwen", "tokens_prompt": 200, "tokens_completion": 100, "duration_ms": 1500, "timestamp": time.Now().Format(time.RFC3339), "success": true},
		{"agent_id": "pm", "model": "llama", "tokens_prompt": float64(10), "tokens_completion": float64(5), "timestamp": time.Now().Format(time.RFC3339), "success": true},
	}
	summary := computeLLMUsageSummary(records)
	if summary == nil {
		t.Fatal("summary nil")
	}
	g := summary["grand"].(map[string]interface{})
	if g["calls"].(int) != 3 || g["tokens_prompt"].(int) != 310 || g["tokens_completion"].(int) != 155 {
		t.Errorf("grand wrong: %+v", g)
	}
	if g["tokens_total"].(int) != 465 {
		t.Errorf("tokens_total: %+v", g["tokens_total"])
	}
	if summary["record_count"].(int) != 3 {
		t.Error("record_count")
	}
	models := g["by_model"].(map[string]interface{})
	if models["qwen"].(int) != 450 || models["llama"].(int) != 15 {
		t.Errorf("by_model: %+v", models)
	}
	byAgent := summary["by_agent"].(map[string]interface{})
	coder := byAgent["coder-1"].(map[string]interface{})
	if coder["calls"].(int) != 1 || coder["tokens_prompt"].(int) != 100 {
		t.Errorf("coder: %+v", coder)
	}
	pm := byAgent["pm"].(map[string]interface{})
	if pm["calls"].(int) != 2 || pm["tokens_total"].(int) != 315 {
		t.Errorf("pm: %+v", pm)
	}

	records = append(records, map[string]interface{}{"agent_id": "test", "model": "llama", "tokens_prompt": 10, "tokens_completion": 5, "timestamp": time.Now().Format(time.RFC3339)})
	s2 := computeLLMUsageSummary(records)
	if s2["record_count"].(int) != 4 {
		t.Error("after record count")
	}
}

func TestComputeLLMUsageSummaryWindows(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-20 * time.Minute)
	earlier := now.Add(-3 * time.Hour)
	prevMonth := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	records := []map[string]interface{}{
		{"agent_id": "coder-1", "model": "qwen", "tokens_prompt": 100, "tokens_completion": 50, "timestamp": recent.Format(time.RFC3339)},
		{"agent_id": "coder-1", "model": "llama", "tokens_prompt": 10, "tokens_completion": 5, "timestamp": earlier.Format(time.RFC3339)},
		{"agent_id": "pm", "model": "qwen", "tokens_prompt": 7, "tokens_completion": 3, "timestamp": prevMonth.Format(time.RFC3339)},
		{"agent_id": "coder-1", "model": "qwen", "tokens_prompt": 1, "tokens_completion": 1, "timestamp": "not-a-time"},
	}
	summary := computeLLMUsageSummary(records)
	g := summary["grand"].(map[string]interface{})
	if g["calls"].(int) != 4 || g["tokens_prompt"].(int) != 118 || g["tokens_completion"].(int) != 59 {
		t.Fatalf("grand: %+v", g)
	}

	var wantHour, wantToday, wantMTD int
	var hourP, todayP, mtdP int
	for _, r := range records {
		p := usageInt(r["tokens_prompt"])
		ts, _ := r["timestamp"].(string)
		parsed, _ := time.Parse(time.RFC3339, ts)
		if parsed.IsZero() {
			parsed = now
		}
		if now.Sub(parsed) <= time.Hour {
			wantHour++
			hourP += p
		}
		if parsed.Year() == now.Year() && parsed.YearDay() == now.YearDay() {
			wantToday++
			todayP += p
		}
		if parsed.Year() == now.Year() && parsed.Month() == now.Month() {
			wantMTD++
			mtdP += p
		}
	}
	hour := summary["last_hour"].(map[string]interface{})
	if hour["calls"].(int) != wantHour || hour["tokens_prompt"].(int) != hourP {
		t.Errorf("last_hour got %+v want calls=%d prompt=%d", hour, wantHour, hourP)
	}
	today := summary["today"].(map[string]interface{})
	if today["calls"].(int) != wantToday || today["tokens_prompt"].(int) != todayP {
		t.Errorf("today got %+v want calls=%d prompt=%d", today, wantToday, todayP)
	}
	mtd := summary["mtd"].(map[string]interface{})
	if mtd["calls"].(int) != wantMTD || mtd["tokens_prompt"].(int) != mtdP {
		t.Errorf("mtd got %+v want calls=%d prompt=%d", mtd, wantMTD, mtdP)
	}
	// The previous-month record is outside every window. The unparseable
	// timestamp is treated as now, so it is inside all of them.
	if wantHour < 2 || wantMTD < wantHour {
		t.Fatalf("oracle windows hour=%d today=%d mtd=%d", wantHour, wantToday, wantMTD)
	}
	if prevMonth.Year() == now.Year() && prevMonth.Month() == now.Month() {
		t.Fatal("fixture previous month collapsed into this month")
	}
}

func useLLMUsage(t *testing.T) {
	t.Helper()
	resetLLMUsageRecords()
	t.Cleanup(resetLLMUsageRecords)
}

func TestLLMUsageRecordSummaryRecentHandlers(t *testing.T) {
	useLLMUsage(t)
	now := time.Now().UTC().Format(time.RFC3339)
	mk := func(agent, model string, prompt, completion int, success bool) map[string]interface{} {
		return map[string]interface{}{
			"agent_id": agent, "model": model, "timestamp": now,
			"tokens_prompt": float64(prompt), "tokens_completion": float64(completion),
			"duration_ms": float64(8), "success": success,
		}
	}
	for _, rec := range []map[string]interface{}{
		mk("coder-1", "qwen", 100, 50, true),
		mk("pm", "llama", 10, 5, false),
	} {
		resp := handleLLMUsageRecord(Message{Source: "network-boundary", Command: "llm.usage.record", Payload: rec})
		if resp.Command != "llm.usage.recorded" {
			t.Fatalf("record command %q payload %#v", resp.Command, resp.Payload)
		}
	}

	summary := handleLLMUsageSummary(Message{Payload: map[string]interface{}{}})
	if summary.Command != "llm.usage.summary" {
		t.Fatal(summary.Command)
	}
	body := summary.Payload.(map[string]interface{})
	g := body["grand"].(map[string]interface{})
	if g["calls"].(int) != 2 || g["tokens_prompt"].(int) != 110 || g["tokens_total"].(int) != 165 {
		t.Fatalf("grand %+v", g)
	}
	filtered := handleLLMUsageSummary(Message{Payload: map[string]interface{}{"agent_id": "pm"}})
	fb := filtered.Payload.(map[string]interface{})
	if fb["record_count"].(int) != 1 {
		t.Fatalf("filter count %+v", fb["record_count"])
	}
	fg := fb["grand"].(map[string]interface{})
	if fg["calls"].(int) != 1 || fg["tokens_prompt"].(int) != 10 {
		t.Fatalf("filtered grand %+v", fg)
	}
	if _, ok := fb["by_agent"].(map[string]interface{})["coder-1"]; ok {
		t.Fatal("agent filter leaked coder-1")
	}
	empty := handleLLMUsageSummary(Message{Payload: map[string]interface{}{"agent_id": "missing"}})
	if empty.Payload.(map[string]interface{})["record_count"].(int) != 0 {
		t.Fatal("unknown agent should be empty")
	}

	recent := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(1)}})
	if recent.Command != "llm.usage.recent" {
		t.Fatal(recent.Command)
	}
	rows := recent.Payload.([]map[string]interface{})
	if len(rows) != 1 || rows[0]["agent_id"] != "pm" {
		t.Fatalf("recent limit 1: %+v", rows)
	}
	recentInt := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": 1}})
	if len(recentInt.Payload.([]map[string]interface{})) != 1 {
		t.Fatal("int limit")
	}
}

func TestLLMUsageRecentLimitCapAndDefault(t *testing.T) {
	useLLMUsage(t)
	for i := 0; i < 510; i++ {
		resp := handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Command: "llm.usage.record",
			Payload: map[string]interface{}{"agent_id": "coder-1", "model": "m", "tokens_prompt": i, "success": true},
		})
		if resp.Command != "llm.usage.recorded" {
			t.Fatal(resp.Command)
		}
	}
	capped := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(10000)}})
	rows := capped.Payload.([]map[string]interface{})
	if len(rows) != 500 {
		t.Fatalf("recent cap = %d, want 500", len(rows))
	}
	if rows[len(rows)-1]["tokens_prompt"].(int) != 509 {
		t.Fatalf("newest record = %#v", rows[len(rows)-1]["tokens_prompt"])
	}
	if rows[0]["tokens_prompt"].(int) != 10 {
		t.Fatalf("oldest of the capped window = %#v, want 10", rows[0]["tokens_prompt"])
	}
	def := handleLLMUsageRecent(Message{})
	if len(def.Payload.([]map[string]interface{})) != 50 {
		t.Fatalf("default recent = %d, want 50", len(def.Payload.([]map[string]interface{})))
	}
	neg := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(-3)}})
	if len(neg.Payload.([]map[string]interface{})) != 50 {
		t.Fatal("negative limit should use the default")
	}
}

func TestLLMUsageRecordCap(t *testing.T) {
	useLLMUsage(t)
	for i := 0; i < 10001; i++ {
		resp := handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Command: "llm.usage.record",
			Payload: map[string]interface{}{"model": itoa(i), "tokens_prompt": 1, "success": true},
		})
		if resp.Command != "llm.usage.recorded" {
			t.Fatalf("i=%d: %s", i, resp.Command)
		}
	}
	got := llmUsageSnapshot()
	if len(got) != 5000 {
		t.Fatalf("after 10001 records len=%d, want 5000", len(got))
	}
	if got[0]["model"] != "5001" || got[len(got)-1]["model"] != "10000" {
		t.Fatalf("kept window %v .. %v", got[0]["model"], got[len(got)-1]["model"])
	}
}

func TestLLMUsageRecordRejectsNonBoundarySource(t *testing.T) {
	useLLMUsage(t)
	payload := map[string]interface{}{"agent_id": "coder-1", "model": "qwen", "tokens_prompt": 1, "success": true}
	for _, src := range []string{"", "store", "daemon-internal", "web-portal", "coder-1"} {
		resp := handleLLMUsageRecord(Message{Source: src, Command: "llm.usage.record", Payload: payload})
		if resp.Command != "error" {
			t.Errorf("source %q command = %q, want error", src, resp.Command)
		}
	}
	if n := len(llmUsageSnapshot()); n != 0 {
		t.Fatalf("rejected sources stored %d records", n)
	}
	ok := handleLLMUsageRecord(Message{Source: "network-boundary", Command: "llm.usage.record", Payload: payload})
	if ok.Command != "llm.usage.recorded" {
		t.Fatal(ok.Command)
	}
	if n := len(llmUsageSnapshot()); n != 1 {
		t.Fatalf("accepted record count %d", n)
	}
}

func TestLLMUsageRecordAllowlistAndCaps(t *testing.T) {
	useLLMUsage(t)
	long := strings.Repeat("a", 300)
	resp := handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Command: "llm.usage.record",
		Payload: map[string]interface{}{
			"agent_id":          long,
			"model":             "qwen",
			"timestamp":         "2026-10-06T00:00:00Z",
			"tokens_prompt":     float64(4),
			"tokens_completion": float64(-1),
			"duration_ms":       "nope",
			"success":           false,
			"error":             strings.Repeat("e", 300),
			"prompt":            "secret prompt",
			"api_key":           "nope",
		},
	})
	if resp.Command != "llm.usage.recorded" {
		t.Fatal(resp.Payload)
	}
	rec := llmUsageSnapshot()[0]
	allowed := map[string]bool{
		"agent_id": true, "model": true, "timestamp": true, "tokens_prompt": true,
		"tokens_completion": true, "duration_ms": true, "success": true, "error": true,
	}
	for k := range rec {
		if !allowed[k] {
			t.Errorf("stored disallowed field %q", k)
		}
	}
	if _, ok := rec["prompt"]; ok {
		t.Fatal("prompt stored")
	}
	if got := rec["agent_id"].(string); len(got) != 256 {
		t.Fatalf("agent_id len %d", len(got))
	}
	if got := rec["error"].(string); len(got) != 256 {
		t.Fatalf("error len %d", len(got))
	}
	if rec["tokens_prompt"].(int) != 4 {
		t.Fatalf("tokens_prompt %#v", rec["tokens_prompt"])
	}
	if _, ok := rec["tokens_completion"]; ok {
		t.Fatal("negative tokens_completion stored")
	}
	if _, ok := rec["duration_ms"]; ok {
		t.Fatal("non-numeric duration stored")
	}
	if rec["success"] != false {
		t.Fatalf("success %#v", rec["success"])
	}

	resetLLMUsageRecords()
	bad := handleLLMUsageRecord(Message{Source: "network-boundary", Payload: "nope"})
	if bad.Command != "error" {
		t.Fatal(bad.Command)
	}
	if len(llmUsageSnapshot()) != 0 {
		t.Fatal("invalid payload stored")
	}
	resetLLMUsageRecords()
	handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Payload: map[string]interface{}{"success": "yes", "tokens_prompt": float64(-5), "note": "x"},
	})
	only := llmUsageSnapshot()[0]
	if _, ok := only["success"]; ok {
		t.Fatal("non-bool success stored")
	}
	if _, ok := only["note"]; ok {
		t.Fatal("note stored")
	}
	if _, ok := only["timestamp"].(string); !ok {
		t.Fatal("missing timestamp fill-in")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
