package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
)

// ─── Trace types ──────────────────────────────────────────────────────────────

// TraceEventType enumerates the kinds of events that appear in a ReAct trace.
type TraceEventType string

const (
	TraceEventThought      TraceEventType = "thought"
	TraceEventToolCalled   TraceEventType = "tool_called"
	TraceEventToolResult   TraceEventType = "tool_result"
	TraceEventTaskProgress TraceEventType = "task_progress"
	TraceEventTaskComplete TraceEventType = "task_complete"
)

// TraceEvent records one step in the ReAct loop for assertion and comparison.
type TraceEvent struct {
	// Type is the kind of event (thought, tool_called, …).
	Type TraceEventType `json:"type"`

	// Tool is the qualified tool name, present when Type==TraceEventToolCalled
	// or TraceEventToolResult.
	Tool string `json:"tool,omitempty"`

	// Args is the raw JSON arguments string for a tool call.
	Args string `json:"args,omitempty"`

	// Content is the LLM-generated text for thought/answer events.
	Content string `json:"content,omitempty"`

	// Success indicates whether a tool call succeeded.
	Success *bool `json:"success,omitempty"`

	// Timestamp is recorded when capturing a live trace.
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// ReActTrace is the full record of one agent task execution — from the initial
// user message through to the final answer.
type ReActTrace struct {
	// TraceID is a correlation ID that flows through the full pipeline.
	TraceID string `json:"trace_id,omitempty"`

	// Input is the original user message.
	Input string `json:"input"`

	// Events is the ordered sequence of steps the agent took.
	Events []TraceEvent `json:"events"`

	// FinalAnswer is the assistant's final content.
	FinalAnswer string `json:"final_answer"`

	// ToolCallCount is the number of tool calls made during the task.
	ToolCallCount int `json:"tool_call_count"`

	// Iterations is the number of LLM calls made before a final answer.
	Iterations int `json:"iterations"`
}

// ─── Golden-trace assertions ──────────────────────────────────────────────────

// AssertGoldenTrace compares got against a stored "golden" JSON snapshot.
//
// Comparison rules:
//   - Event sequence length must match exactly.
//   - Event types must match exactly (deterministic fields).
//   - Tool names and args are compared exactly.
//   - LLM-generated text (Content, FinalAnswer) uses fuzzy token overlap
//     (≥ goldenTextSimilarityThreshold) so minor phrasing differences don't
//     break the snapshot.
//
// Set the environment variable UPDATE_SNAPSHOTS=1 to write/update the golden
// file instead of comparing.
//
// Golden files are stored under testdata/golden/<name>.json relative to the
// package root.
func AssertGoldenTrace(t *testing.T, got ReActTrace, name string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "golden", name+".json")

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatalf("AssertGoldenTrace: marshal: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0750); err != nil {
			t.Fatalf("AssertGoldenTrace: mkdirall: %v", err)
		}
		if err := os.WriteFile(goldenPath, data, 0600); err != nil {
			t.Fatalf("AssertGoldenTrace: write golden: %v", err)
		}
		t.Logf("AssertGoldenTrace: updated golden file %s", goldenPath)
		return
	}

	data, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf(
				"AssertGoldenTrace: golden file %q does not exist.\n"+
					"Run with UPDATE_SNAPSHOTS=1 to create it.",
				goldenPath,
			)
		}
		t.Fatalf("AssertGoldenTrace: read golden: %v", err)
	}

	var want ReActTrace
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("AssertGoldenTrace: unmarshal golden: %v", err)
	}

	compareTraces(t, want, got)
}

const goldenTextSimilarityThreshold = 0.90

// uuidPattern matches full UUIDs and 8-char hex ID prefixes in any text.
// Both forms appear in logs and progress messages and are non-deterministic.
var uuidPattern = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` +
		`|[0-9a-fA-F]{8}`,
)

// normalizeIDs replaces all UUID-like strings in s with the placeholder "<id>"
// so that golden comparisons are not affected by non-deterministic identifiers.
func normalizeIDs(s string) string {
	return uuidPattern.ReplaceAllString(s, "<id>")
}

// compareTraces applies the mixed exact/fuzzy comparison rules.
func compareTraces(t *testing.T, want, got ReActTrace) {
	t.Helper()

	if want.Input != got.Input {
		t.Errorf("golden: Input mismatch\n  want: %q\n   got: %q", want.Input, got.Input)
	}
	if want.ToolCallCount != got.ToolCallCount {
		t.Errorf("golden: ToolCallCount mismatch: want %d, got %d", want.ToolCallCount, got.ToolCallCount)
	}
	if want.Iterations != got.Iterations {
		t.Errorf("golden: Iterations mismatch: want %d, got %d", want.Iterations, got.Iterations)
	}

	// Fuzzy match on final answer (normalize non-deterministic IDs first).
	if sim := tokenOverlap(normalizeIDs(want.FinalAnswer), normalizeIDs(got.FinalAnswer)); sim < goldenTextSimilarityThreshold {
		t.Errorf(
			"golden: FinalAnswer similarity %.2f < threshold %.2f\n  want: %q\n   got: %q",
			sim, goldenTextSimilarityThreshold, want.FinalAnswer, got.FinalAnswer,
		)
	}

	if len(want.Events) != len(got.Events) {
		t.Errorf("golden: event count mismatch: want %d, got %d", len(want.Events), len(got.Events))
		// Still compare the events we have.
		n := len(want.Events)
		if len(got.Events) < n {
			n = len(got.Events)
		}
		want.Events = want.Events[:n]
		got.Events = got.Events[:n]
	}

	for i, we := range want.Events {
		if i >= len(got.Events) {
			break
		}
		ge := got.Events[i]
		pos := fmt.Sprintf("event[%d]", i)

		if we.Type != ge.Type {
			t.Errorf("golden: %s Type mismatch: want %q, got %q", pos, we.Type, ge.Type)
		}
		if we.Tool != ge.Tool {
			t.Errorf("golden: %s Tool mismatch: want %q, got %q", pos, we.Tool, ge.Tool)
		}
		// Args may contain non-deterministic UUIDs; normalize before comparing.
		if we.Args != "" && normalizeIDs(we.Args) != normalizeIDs(ge.Args) {
			t.Errorf("golden: %s Args mismatch\n  want: %q\n   got: %q", pos, we.Args, ge.Args)
		}
		if we.Content != "" {
			if sim := tokenOverlap(normalizeIDs(we.Content), normalizeIDs(ge.Content)); sim < goldenTextSimilarityThreshold {
				t.Errorf(
					"golden: %s Content similarity %.2f < %.2f\n  want: %q\n   got: %q",
					pos, sim, goldenTextSimilarityThreshold, we.Content, ge.Content,
				)
			}
		}
		if we.Success != nil && ge.Success != nil && *we.Success != *ge.Success {
			t.Errorf("golden: %s Success mismatch: want %v, got %v", pos, *we.Success, *ge.Success)
		}
	}
}

// tokenOverlap returns the Jaccard coefficient of the word-token sets of a and b.
// Returns 1.0 when both strings are empty.
func tokenOverlap(a, b string) float64 {
	ta := tokenSet(a)
	tb := tokenSet(b)
	if len(ta) == 0 && len(tb) == 0 {
		return 1.0
	}
	intersection := 0
	for t := range ta {
		if tb[t] {
			intersection++
		}
	}
	union := len(ta) + len(tb) - intersection
	if union == 0 {
		return 1.0
	}
	return math.Round(float64(intersection)/float64(union)*100) / 100
}

// tokenSet splits s into lower-case words and returns the unique set.
func tokenSet(s string) map[string]bool {
	s = strings.ToLower(s)
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	m := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 1 {
			m[w] = true
		}
	}
	return m
}

// ─── ReActTrace builder helpers ───────────────────────────────────────────────

// formatToolCallBlock formats a tool call as the assistant message content
// that is appended to the conversation after each tool invocation.
// This format must match the production format in chat_handlers.go so that
// the guest-agent can parse it correctly.
func formatToolCallBlock(tool, args string) string {
	return fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", tool, args)
}

// traceRecorder captures ReAct events into a ReActTrace.
// It is used by journey and in-process tests to build a trace for golden
// comparison or direct assertion.
type traceRecorder struct {
	trace ReActTrace
}

func newTraceRecorder(traceID, input string) *traceRecorder {
	return &traceRecorder{
		trace: ReActTrace{
			TraceID: traceID,
			Input:   input,
		},
	}
}

func (r *traceRecorder) recordThought(content string) {
	r.trace.Events = append(r.trace.Events, TraceEvent{
		Type:      TraceEventThought,
		Content:   content,
		Timestamp: time.Now().UTC(),
	})
	r.trace.Iterations++
}

func boolPtr(b bool) *bool { return &b }

func (r *traceRecorder) recordToolCall(tool, args string) {
	r.trace.Events = append(r.trace.Events, TraceEvent{
		Type:      TraceEventToolCalled,
		Tool:      tool,
		Args:      args,
		Timestamp: time.Now().UTC(),
	})
	r.trace.ToolCallCount++
}

func (r *traceRecorder) recordToolResult(tool string, success bool) {
	r.trace.Events = append(r.trace.Events, TraceEvent{
		Type:      TraceEventToolResult,
		Tool:      tool,
		Success:   boolPtr(success),
		Timestamp: time.Now().UTC(),
	})
}

func (r *traceRecorder) recordProgress(content string) {
	r.trace.Events = append(r.trace.Events, TraceEvent{
		Type:      TraceEventTaskProgress,
		Content:   content,
		Timestamp: time.Now().UTC(),
	})
}

func (r *traceRecorder) finalize(finalAnswer string) ReActTrace {
	r.trace.Events = append(r.trace.Events, TraceEvent{
		Type:      TraceEventTaskComplete,
		Content:   finalAnswer,
		Timestamp: time.Now().UTC(),
	})
	r.trace.FinalAnswer = finalAnswer
	return r.trace
}

// filterEventsByType returns all trace events of the given type.
// Defined here (golden_test.go) rather than in the inprocesstest-tagged file so
// that react_journey_test.go and portal_contract_test.go can use it too.
func filterEventsByType(events []TraceEvent, typ TraceEventType) []TraceEvent {
var out []TraceEvent
for _, e := range events {
if e.Type == typ {
out = append(out, e)
}
}
return out
}
