package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func assertNoLLMUsageReply(t *testing.T, resp Message) {
	t.Helper()
	if resp.Command != "" || resp.Payload != nil {
		t.Fatalf("reply command %q payload %#v, want no reply", resp.Command, resp.Payload)
	}
}

func recentRows(t *testing.T, msg Message) []map[string]interface{} {
	t.Helper()
	body, ok := msg.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("recent payload type %T, want wrapper", msg.Payload)
	}
	rows, ok := body["records"].([]map[string]interface{})
	if !ok && body["records"] != nil {
		t.Fatalf("records type %T", body["records"])
	}
	return rows
}

func recentLastSeq(t *testing.T, msg Message) uint64 {
	t.Helper()
	body, ok := msg.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("recent payload type %T", msg.Payload)
	}
	seq, ok := usageSeqValue(body["last_seq"])
	if !ok {
		t.Fatalf("last_seq %#v", body["last_seq"])
	}
	return seq
}

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
		assertNoLLMUsageReply(t, resp)
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
	rows := recentRows(t, recent)
	if len(rows) != 1 || rows[0]["agent_id"] != "pm" {
		t.Fatalf("recent limit 1: %+v", rows)
	}
	recentInt := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": 1}})
	if len(recentRows(t, recentInt)) != 1 {
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
		assertNoLLMUsageReply(t, resp)
	}
	if llmUsageRecentMax != 500 || llmUsageRecentDefault != 50 {
		t.Fatalf("recent limits max=%d default=%d, want 500 and 50", llmUsageRecentMax, llmUsageRecentDefault)
	}
	capped := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(10000)}})
	rows := recentRows(t, capped)
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
	if n := len(recentRows(t, def)); n != 50 {
		t.Fatalf("default recent = %d, want 50", n)
	}
	neg := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(-3)}})
	if len(recentRows(t, neg)) != 50 {
		t.Fatal("negative limit should use the default")
	}
}

func TestLLMUsageRecentFiltersAgentID(t *testing.T) {
	useLLMUsage(t)
	for i, agent := range []string{"coder-1", "pm", "coder-1"} {
		resp := handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Command: "llm.usage.record",
			Payload: map[string]interface{}{"agent_id": agent, "model": "qwen", "tokens_prompt": i, "success": true},
		})
		assertNoLLMUsageReply(t, resp)
	}
	recent := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"agent_id": "pm", "limit": float64(10)}})
	rows := recentRows(t, recent)
	if len(rows) != 1 || rows[0]["agent_id"] != "pm" {
		t.Fatalf("filtered recent %+v", rows)
	}
	limited := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"agent_id": "coder-1", "limit": float64(1)}})
	rows = recentRows(t, limited)
	if len(rows) != 1 || rows[0]["tokens_prompt"].(int) != 2 {
		t.Fatalf("newest coder record %+v", rows)
	}
	// pm now holds the newest rows. Limit runs after the agent filter, so
	// coder-1's newest row is kept. Limit-before-filter would return only pm.
	for _, tokens := range []int{100, 101, 102} {
		resp := handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Command: "llm.usage.record",
			Payload: map[string]interface{}{"agent_id": "pm", "model": "qwen", "tokens_prompt": tokens, "success": true},
		})
		assertNoLLMUsageReply(t, resp)
	}
	limited = handleLLMUsageRecent(Message{Payload: map[string]interface{}{"agent_id": "coder-1", "limit": float64(1)}})
	rows = recentRows(t, limited)
	if len(rows) != 1 || rows[0]["agent_id"] != "coder-1" || rows[0]["tokens_prompt"].(int) != 2 {
		t.Fatalf("newest coder after newer pm rows %+v", rows)
	}
	two := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"agent_id": "coder-1", "limit": float64(2)}})
	rows = recentRows(t, two)
	if len(rows) != 2 || rows[0]["tokens_prompt"].(int) != 0 || rows[1]["tokens_prompt"].(int) != 2 {
		t.Fatalf("newest two coder records %+v", rows)
	}
	for _, row := range rows {
		if row["agent_id"] != "coder-1" {
			t.Fatalf("other agent leaked into coder window %+v", rows)
		}
	}
}

// TestLLMUsageRecordRejectsDaemonInternalSource pins the Store backstop.
// The ACL now denies llm.usage.record from daemon-internal too.
// Store still rejects that source if a frame gets through, and does not reply:
// llm.usage.record is a one-way push.
func TestLLMUsageRecordRejectsDaemonInternalSource(t *testing.T) {
	useLLMUsage(t)
	resp := handleLLMUsageRecord(Message{
		Source:  "daemon-internal",
		Command: "llm.usage.record",
		Payload: map[string]interface{}{"agent_id": "coder-1", "model": "qwen", "tokens_prompt": 1, "success": true},
	})
	assertNoLLMUsageReply(t, resp)
	if n := len(llmUsageSnapshot()); n != 0 {
		t.Fatalf("daemon-internal record stored %d", n)
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
		assertNoLLMUsageReply(t, resp)
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
		assertNoLLMUsageReply(t, resp)
	}
	if n := len(llmUsageSnapshot()); n != 0 {
		t.Fatalf("rejected sources stored %d records", n)
	}
	ok := handleLLMUsageRecord(Message{Source: "network-boundary", Command: "llm.usage.record", Payload: payload})
	assertNoLLMUsageReply(t, ok)
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
	assertNoLLMUsageReply(t, resp)
	rec := llmUsageSnapshot()[0]
	allowed := map[string]bool{
		"agent_id": true, "model": true, "timestamp": true, "tokens_prompt": true,
		"tokens_completion": true, "duration_ms": true, "success": true, "error": true,
		"seq": true,
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
	if rec["seq"] != uint64(1) {
		t.Fatalf("seq %#v, want store-assigned 1", rec["seq"])
	}

	resetLLMUsageRecords()
	bad := handleLLMUsageRecord(Message{Source: "network-boundary", Payload: "nope"})
	assertNoLLMUsageReply(t, bad)
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

func TestLLMUsageRecordNoReplyStoresOnlyValid(t *testing.T) {
	useLLMUsage(t)
	valid := handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Command: "llm.usage.record",
		Payload: map[string]interface{}{"agent_id": "coder-1", "model": "qwen", "tokens_prompt": 1, "success": true},
	})
	assertNoLLMUsageReply(t, valid)
	if len(llmUsageSnapshot()) != 1 {
		t.Fatal("valid record not stored")
	}
	for _, msg := range []Message{
		{Source: "coder-1", Command: "llm.usage.record", Payload: map[string]interface{}{"tokens_prompt": 1}},
		{Source: "network-boundary", Command: "llm.usage.record", Payload: "nope"},
		{Source: "network-boundary", Command: "llm.usage.record", Payload: nil},
	} {
		assertNoLLMUsageReply(t, handleLLMUsageRecord(msg))
	}
	if len(llmUsageSnapshot()) != 1 {
		t.Fatalf("invalid records stored, len=%d", len(llmUsageSnapshot()))
	}
}

func TestLLMUsageNumericCapDropsOverflow(t *testing.T) {
	useLLMUsage(t)
	var maxF float64 = llmUsageMaxNumeric
	maxN := int(maxF)
	if maxN != 1000000000000 {
		t.Fatalf("cap int = %d", maxN)
	}
	dropped := []interface{}{
		math.Ldexp(1, 63), // 2^63, the float rounding hole
		9.3e18,
		1e13,
		maxF + 1,
		float64(-1),
		int(-1),
		int64(-1),
		int32(-1),
		int(1e13),
		int64(1e13),
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
		json.Number("1e13"),
		json.Number("9223372036854775808"),
		json.Number("1e20"),
	}
	for _, v := range dropped {
		resetLLMUsageRecords()
		resp := handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Payload: map[string]interface{}{"model": "m", "tokens_prompt": v},
		})
		assertNoLLMUsageReply(t, resp)
		rec := llmUsageSnapshot()[0]
		if _, ok := rec["tokens_prompt"]; ok {
			t.Fatalf("tokens_prompt stored for %#v (%T): %#v", v, v, rec["tokens_prompt"])
		}
	}

	for _, v := range []interface{}{
		maxF,
		maxN,
		int64(maxN),
		int32(7),
		json.Number("1000000000000"),
		json.Number("1e12"),
	} {
		resetLLMUsageRecords()
		resp := handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Payload: map[string]interface{}{"model": "m", "tokens_prompt": v, "tokens_completion": v, "duration_ms": v},
		})
		assertNoLLMUsageReply(t, resp)
		rec := llmUsageSnapshot()[0]
		want := maxN
		if _, isSmall := v.(int32); isSmall {
			want = 7
		}
		for _, key := range []string{"tokens_prompt", "tokens_completion", "duration_ms"} {
			got, ok := rec[key].(int)
			if !ok || got != want {
				t.Fatalf("%s for %#v (%T) = %#v, want %d", key, v, v, rec[key], want)
			}
			if got < 0 {
				t.Fatalf("%s negative: %d", key, got)
			}
		}
	}
}

func TestLLMUsageSummarySumsStayPositiveAtCap(t *testing.T) {
	useLLMUsage(t)
	var maxF float64 = llmUsageMaxNumeric
	maxN := int(maxF)
	const recordsN = 10000
	for i := 0; i < recordsN; i++ {
		resp := handleLLMUsageRecord(Message{
			Source: "network-boundary",
			Payload: map[string]interface{}{
				"agent_id": "coder-1", "model": "qwen", "success": true,
				"tokens_prompt": maxN, "tokens_completion": maxN,
			},
		})
		assertNoLLMUsageReply(t, resp)
	}
	if len(llmUsageSnapshot()) != recordsN {
		t.Fatalf("stored %d, want %d (the 10001st record trims the log)", len(llmUsageSnapshot()), recordsN)
	}

	body := handleLLMUsageSummary(Message{}).Payload.(map[string]interface{})
	g := body["grand"].(map[string]interface{})
	want := recordsN * maxN
	if want <= 0 {
		t.Fatalf("oracle sum overflowed: %d", want)
	}
	if g["tokens_prompt"].(int) != want || g["tokens_completion"].(int) != want {
		t.Fatalf("grand tokens = %v %v, want %d", g["tokens_prompt"], g["tokens_completion"], want)
	}
	total := g["tokens_total"].(int)
	if total != want*2 || total <= 0 {
		t.Fatalf("tokens_total = %d, want %d", total, want*2)
	}
	byModel := g["by_model"].(map[string]interface{})
	if byModel["qwen"].(int) != want*2 || byModel["qwen"].(int) <= 0 {
		t.Fatalf("by_model = %#v", byModel)
	}
	agent := body["by_agent"].(map[string]interface{})["coder-1"].(map[string]interface{})
	if agent["tokens_total"].(int) != want*2 || agent["tokens_total"].(int) <= 0 {
		t.Fatalf("by_agent = %#v", agent)
	}
	if g["calls"].(int) != recordsN || g["calls"].(int) <= 0 {
		t.Fatalf("calls = %v", g["calls"])
	}
}

func TestLLMUsageTimestampReplacedWhenUnparseableOrFuture(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	exact5 := now.Add(llmUsageFutureSkew).Format(time.RFC3339)
	if got := normalizeUsageTimestamp(exact5, now); got != exact5 {
		t.Fatalf("exactly 5 min ahead = %q, want keep", got)
	}
	over := now.Add(llmUsageFutureSkew + time.Second).Format(time.RFC3339)
	if got := normalizeUsageTimestamp(over, now); got != now.Format(time.RFC3339) {
		t.Fatalf("more than 5 min ahead = %q, want receive time", got)
	}
	if got := normalizeUsageTimestamp("not-a-time", now); got != now.Format(time.RFC3339) {
		t.Fatalf("unparseable = %q, want receive time", got)
	}
	past := "2020-01-02T03:04:05Z"
	if got := normalizeUsageTimestamp(past, now); got != past {
		t.Fatalf("past = %q", got)
	}
	if got := normalizeUsageTimestamp("", now); got != now.Format(time.RFC3339) {
		t.Fatalf("empty = %q", got)
	}

	useLLMUsage(t)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Payload: map[string]interface{}{"timestamp": future, "model": "m", "success": true},
	})
	stored, _ := llmUsageSnapshot()[0]["timestamp"].(string)
	parsed, err := time.Parse(time.RFC3339, stored)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.After(time.Now().Add(2 * time.Minute)) {
		t.Fatalf("future timestamp kept: %s", stored)
	}
	resetLLMUsageRecords()
	soon := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Payload: map[string]interface{}{"timestamp": soon, "model": "m"},
	})
	if got, _ := llmUsageSnapshot()[0]["timestamp"].(string); got != soon {
		t.Fatalf("within skew = %q, want %q", got, soon)
	}
	resetLLMUsageRecords()
	handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Payload: map[string]interface{}{"timestamp": "not-a-time", "model": "m"},
	})
	got, _ := llmUsageSnapshot()[0]["timestamp"].(string)
	if got == "not-a-time" {
		t.Fatal("unparseable timestamp stored")
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatal(err)
	}
}

func TestCappedUsageStringTrimsOnRuneBoundary(t *testing.T) {
	// 255 ASCII bytes, then a 3-byte rune. A 256-byte cut splits 你.
	raw := strings.Repeat("a", 255) + "你" + "zzzz"
	got, ok := cappedUsageString(raw)
	if !ok || !utf8.ValidString(got) || got != strings.Repeat("a", 255) {
		t.Fatalf("trimmed = %q valid=%v", got, utf8.ValidString(got))
	}
	if len(got) > llmUsageMaxString {
		t.Fatalf("len %d", len(got))
	}
}

func TestLLMUsageSeqMonotonicAcrossTrim(t *testing.T) {
	useLLMUsage(t)
	first := handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Command: "llm.usage.record",
		Payload: map[string]interface{}{"agent_id": "coder-1", "model": "m", "tokens_prompt": 0, "seq": float64(99), "success": true},
	})
	assertNoLLMUsageReply(t, first)
	if got := llmUsageSnapshot()[0]["seq"]; got != uint64(1) {
		t.Fatalf("client seq kept as %#v, want 1", got)
	}
	for i := 1; i < 10001; i++ {
		assertNoLLMUsageReply(t, handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Command: "llm.usage.record",
			Payload: map[string]interface{}{"agent_id": "coder-1", "model": "m", "tokens_prompt": i, "success": true},
		}))
	}
	newest := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(1)}})
	rows := recentRows(t, newest)
	if len(rows) != 1 || rows[0]["seq"] != uint64(10001) || rows[0]["tokens_prompt"].(int) != 10000 {
		t.Fatalf("newest after trim %+v", rows)
	}
	if recentLastSeq(t, newest) != 10001 {
		t.Fatalf("last_seq %d, want 10001", recentLastSeq(t, newest))
	}
	// Kept window is seq 5002..10001. Forward page starts at the oldest seq above 5001.
	page := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"after_seq": float64(5001), "limit": float64(1)}})
	rows = recentRows(t, page)
	if len(rows) != 1 || rows[0]["seq"] != uint64(5002) || rows[0]["tokens_prompt"].(int) != 5001 {
		t.Fatalf("oldest after trim boundary %+v", rows)
	}
	assertNoLLMUsageReply(t, handleLLMUsageRecord(Message{
		Source:  "network-boundary",
		Command: "llm.usage.record",
		Payload: map[string]interface{}{"agent_id": "coder-1", "model": "m", "tokens_prompt": 10001, "success": true},
	}))
	again := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"limit": float64(1)}})
	rows = recentRows(t, again)
	if len(rows) != 1 || rows[0]["seq"] != uint64(10002) {
		t.Fatalf("seq rewound across trim %+v last_seq=%d", rows, recentLastSeq(t, again))
	}
}

func TestLLMUsageRecentAfterSeqPagesForward(t *testing.T) {
	useLLMUsage(t)
	// Oldest first: pm, coder, coder, pm, coder.
	for i, agent := range []string{"pm", "coder-1", "coder-1", "pm", "coder-1"} {
		assertNoLLMUsageReply(t, handleLLMUsageRecord(Message{
			Source:  "network-boundary",
			Command: "llm.usage.record",
			Payload: map[string]interface{}{"agent_id": agent, "model": "m", "tokens_prompt": i, "success": true},
		}))
	}
	// No after_seq: newest limit, so the last coder (seq 5), not the oldest.
	newest := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"agent_id": "coder-1", "limit": float64(1)}})
	rows := recentRows(t, newest)
	if len(rows) != 1 || rows[0]["seq"] != uint64(5) {
		t.Fatalf("newest coder %+v", rows)
	}
	// after_seq walks forward: oldest coder with seq > 0 is seq 2, not seq 5.
	forward := handleLLMUsageRecent(Message{Payload: map[string]interface{}{
		"agent_id": "coder-1", "after_seq": float64(0), "limit": float64(1),
	}})
	rows = recentRows(t, forward)
	if len(rows) != 1 || rows[0]["seq"] != uint64(2) || rows[0]["agent_id"] != "coder-1" {
		t.Fatalf("oldest coder page %+v", rows)
	}
	if recentLastSeq(t, forward) != 5 {
		t.Fatalf("last_seq %d, want 5 even on a filtered page", recentLastSeq(t, forward))
	}
	rest := handleLLMUsageRecent(Message{Payload: map[string]interface{}{
		"agent_id": "coder-1", "after_seq": float64(2), "limit": float64(10),
	}})
	rows = recentRows(t, rest)
	if len(rows) != 2 || rows[0]["seq"] != uint64(3) || rows[1]["seq"] != uint64(5) {
		t.Fatalf("coder page after seq 2 %+v", rows)
	}
	// Limit applies after the agent filter and takes the oldest matches, not the newest.
	two := handleLLMUsageRecent(Message{Payload: map[string]interface{}{
		"after_seq": float64(1), "limit": float64(2),
	}})
	rows = recentRows(t, two)
	if len(rows) != 2 || rows[0]["seq"] != uint64(2) || rows[1]["seq"] != uint64(3) {
		t.Fatalf("oldest two after seq 1 %+v", rows)
	}
	empty := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"after_seq": "nope", "limit": float64(10)}})
	if rows = recentRows(t, empty); len(rows) != 0 || recentLastSeq(t, empty) != 5 {
		t.Fatalf("invalid after_seq page %+v last=%d", rows, recentLastSeq(t, empty))
	}
	tail := handleLLMUsageRecent(Message{Payload: map[string]interface{}{"after_seq": uint64(5), "limit": 10}})
	if rows = recentRows(t, tail); len(rows) != 0 || recentLastSeq(t, tail) != 5 {
		t.Fatalf("caught up page %+v last=%d", rows, recentLastSeq(t, tail))
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
