package main

// portal_contract_test.go — Contract tests for ToolCallEvent and ThoughtEvent
// JSON payloads consumed by the dashboard.
//
// These tests assert the exact JSON shape of portal events so that any
// accidental field rename, removal, or type change is caught before it breaks
// the dashboard UI.
//
// Run with:
//
//	go test ./cmd/aegisclaw/ -run 'TestPortalContract' -v

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// ─── ToolCallEvent contract ───────────────────────────────────────────────────

// TestPortalContractToolCallEvent_StartShape verifies the JSON shape of a
// ToolCallEvent "start" payload as it would be read by the dashboard.
func TestPortalContractToolCallEvent_StartShape(t *testing.T) {
	buf := NewToolEventBuffer(10)
	buf.RecordStart("proposal.create_draft")

	events := buf.Recent(5)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data, err := json.Marshal(events[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	// Required fields.
	requiredFields := []string{"id", "timestamp", "tool", "phase", "success"}
	for _, f := range requiredFields {
		if _, ok := m[f]; !ok {
			t.Errorf("ToolCallEvent start missing required field %q; JSON: %s", f, data)
		}
	}

	// Field values.
	if m["tool"] != "proposal.create_draft" {
		t.Errorf("tool = %v, want 'proposal.create_draft'", m["tool"])
	}
	if m["phase"] != "start" {
		t.Errorf("phase = %v, want 'start'", m["phase"])
	}
	// success should be true for start events (they haven't failed yet).
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	// id must be a positive number.
	idVal, ok := m["id"].(float64)
	if !ok || idVal <= 0 {
		t.Errorf("id = %v, want positive number", m["id"])
	}
	// timestamp must be parseable as RFC3339.
	tsStr, _ := m["timestamp"].(string)
	if tsStr == "" {
		t.Error("timestamp field is missing or empty")
	} else if _, err := time.Parse(time.RFC3339Nano, tsStr); err != nil {
		t.Errorf("timestamp %q not RFC3339: %v", tsStr, err)
	}
	// error field must be absent (not nil, but absent).
	if _, present := m["error"]; present {
		t.Errorf("error field should be absent in start event; JSON: %s", data)
	}
	// duration_ms must be absent (not yet measured).
	if _, present := m["duration_ms"]; present {
		t.Errorf("duration_ms should be absent in start event; JSON: %s", data)
	}
}

// TestPortalContractToolCallEvent_FinishSuccessShape verifies the JSON shape of
// a successful ToolCallEvent "finish" payload.
func TestPortalContractToolCallEvent_FinishSuccessShape(t *testing.T) {
	buf := NewToolEventBuffer(10)
	buf.RecordStart("proposal.submit")
	buf.RecordFinish("proposal.submit", true, nil, 55*time.Millisecond)

	events := buf.Recent(10)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (start+finish), got %d", len(events))
	}

	finish := events[1]
	data, _ := json.Marshal(finish)
	var m map[string]interface{}
	json.Unmarshal(data, &m) //nolint:errcheck

	if m["phase"] != "finish" {
		t.Errorf("phase = %v, want 'finish'", m["phase"])
	}
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	durMs, ok := m["duration_ms"].(float64)
	if !ok {
		t.Errorf("duration_ms missing or wrong type; JSON: %s", data)
	} else if durMs != 55 {
		t.Errorf("duration_ms = %v, want 55", durMs)
	}
	if _, present := m["error"]; present {
		t.Errorf("error field should be absent on success; JSON: %s", data)
	}
}

// TestPortalContractToolCallEvent_FinishFailureShape verifies the JSON shape of
// a failed ToolCallEvent "finish" payload.
func TestPortalContractToolCallEvent_FinishFailureShape(t *testing.T) {
	buf := NewToolEventBuffer(10)
	buf.RecordStart("proposal.bad_tool")
	buf.RecordFinish("proposal.bad_tool", false, errors.New("unknown tool: proposal.bad_tool"), 3*time.Millisecond)

	events := buf.Recent(10)
	finish := events[1]
	data, _ := json.Marshal(finish)
	var m map[string]interface{}
	json.Unmarshal(data, &m) //nolint:errcheck

	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	errStr, _ := m["error"].(string)
	if !strings.Contains(errStr, "unknown tool") {
		t.Errorf("error field missing 'unknown tool'; got %q", errStr)
	}
}

// TestPortalContractToolCallEvent_IDMonotonicity verifies that event IDs are
// strictly increasing across a sequence of events.
func TestPortalContractToolCallEvent_IDMonotonicity(t *testing.T) {
	buf := NewToolEventBuffer(20)
	tools := []string{"proposal.create_draft", "proposal.submit", "proposal.status"}
	for _, tool := range tools {
		buf.RecordStart(tool)
		buf.RecordFinish(tool, true, nil, time.Millisecond)
	}

	events := buf.Recent(20)
	if len(events) < 6 {
		t.Fatalf("expected at least 6 events, got %d", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Errorf("event[%d].ID (%d) not > event[%d].ID (%d)",
				i, events[i].ID, i-1, events[i-1].ID)
		}
	}
}

// ─── ThoughtEvent contract ────────────────────────────────────────────────────

// TestPortalContractThoughtEvent_Shape verifies the JSON shape of a ThoughtEvent
// payload as the dashboard reads it.
func TestPortalContractThoughtEvent_Shape(t *testing.T) {
	buf := NewThoughtEventBuffer(10)
	buf.Record("tool_call", "proposal.create_draft", "Decided to call tool: proposal.create_draft", "Model reasoning here.")

	events := buf.Recent(5)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data, _ := json.Marshal(events[0])
	var m map[string]interface{}
	json.Unmarshal(data, &m) //nolint:errcheck

	// Required fields for dashboard rendering.
	requiredFields := []string{"id", "timestamp", "phase", "summary"}
	for _, f := range requiredFields {
		if _, ok := m[f]; !ok {
			t.Errorf("ThoughtEvent missing required field %q; JSON: %s", f, data)
		}
	}

	if m["phase"] != "tool_call" {
		t.Errorf("phase = %v, want 'tool_call'", m["phase"])
	}
	if m["tool"] != "proposal.create_draft" {
		t.Errorf("tool = %v, want 'proposal.create_draft'", m["tool"])
	}
	summaryStr, _ := m["summary"].(string)
	if !strings.Contains(summaryStr, "proposal.create_draft") {
		t.Errorf("summary should mention tool name; got %q", summaryStr)
	}
	detailsStr, _ := m["details"].(string)
	if detailsStr == "" {
		t.Error("details should not be empty when reasoning is provided")
	}
}

// TestPortalContractThoughtEvent_ModelThinkingPhase verifies that "model_thinking"
// phase events omit the tool field (since thinking is not tool-specific).
func TestPortalContractThoughtEvent_ModelThinkingPhase(t *testing.T) {
	buf := NewThoughtEventBuffer(10)
	buf.Record("model_thinking", "", "Analyzing the request", "The model is reasoning about the user's intent.")

	events := buf.Recent(5)
	data, _ := json.Marshal(events[0])
	var m map[string]interface{}
	json.Unmarshal(data, &m) //nolint:errcheck

	if m["phase"] != "model_thinking" {
		t.Errorf("phase = %v, want 'model_thinking'", m["phase"])
	}
	// tool must be absent (empty string is omitempty → absent in JSON).
	if tool, present := m["tool"]; present {
		t.Errorf("tool field should be absent for model_thinking phase; got %v", tool)
	}
}

// TestPortalContractThoughtEvent_IDMonotonicity verifies thought event IDs are
// strictly increasing.
func TestPortalContractThoughtEvent_IDMonotonicity(t *testing.T) {
	buf := NewThoughtEventBuffer(20)
	phases := []struct{ phase, tool, summary, details string }{
		{"model_thinking", "", "Thinking…", "…"},
		{"tool_call", "proposal.create_draft", "Calling create_draft", ""},
		{"tool_result", "proposal.create_draft", "create_draft succeeded", "success=true"},
		{"model_thinking", "", "Analyzing result", ""},
		{"tool_call", "proposal.submit", "Calling submit", ""},
		{"tool_result", "proposal.submit", "submit succeeded", "success=true"},
	}
	for _, p := range phases {
		buf.Record(p.phase, p.tool, p.summary, p.details)
	}

	events := buf.Recent(20)
	if len(events) != len(phases) {
		t.Fatalf("expected %d events, got %d", len(phases), len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Errorf("thought[%d].ID (%d) not > thought[%d].ID (%d)",
				i, events[i].ID, i-1, events[i-1].ID)
		}
	}
}

// TestPortalContractEventBuffer_MaxCapacity verifies that the ring-buffer cap is
// enforced: oldest events are evicted when max is exceeded.
func TestPortalContractEventBuffer_MaxCapacity(t *testing.T) {
	const max = 5
	buf := NewToolEventBuffer(max)
	for i := 0; i < max+3; i++ {
		buf.RecordStart("tool")
	}
	events := buf.Recent(100)
	if len(events) != max {
		t.Errorf("expected %d events (cap), got %d", max, len(events))
	}
	// The remaining events must be the most recent (highest IDs).
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Errorf("ID ordering violated after eviction: [%d]=%d, [%d]=%d",
				i-1, events[i-1].ID, i, events[i].ID)
		}
	}
}

// TestPortalContractThoughtBuffer_MaxCapacity verifies thought event ring-buffer
// eviction.
func TestPortalContractThoughtBuffer_MaxCapacity(t *testing.T) {
	const max = 4
	buf := NewThoughtEventBuffer(max)
	for i := 0; i < max+2; i++ {
		buf.Record("model_thinking", "", "thought", "")
	}
	events := buf.Recent(100)
	if len(events) != max {
		t.Errorf("expected %d events (cap), got %d", max, len(events))
	}
}
