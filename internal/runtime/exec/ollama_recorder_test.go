//go:build inprocesstest
// +build inprocesstest

package exec_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/runtime/exec"
)

// ─── OllamaRecorder: replay ───────────────────────────────────────────────────

// writeFixture writes a cassette JSON file for one agent turn, used by tests
// that run in replay mode.
func writeFixture(t *testing.T, dir, name string, idx int, resp exec.AgentTurnResponse) {
	t.Helper()
	type cassetteTurn struct {
		Request  exec.AgentTurnRequest  `json:"request"`
		Response exec.AgentTurnResponse `json:"response"`
	}
	turn := cassetteTurn{
		Request: exec.AgentTurnRequest{
			Messages: []exec.AgentMessage{{Role: "user", Content: "test"}},
		},
		Response: resp,
	}
	data, err := json.MarshalIndent(turn, "", "  ")
	if err != nil {
		t.Fatalf("writeFixture marshal: %v", err)
	}
	path := filepath.Join(dir, name+"-"+formatIdx(idx)+".json")
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("writeFixture mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("writeFixture write: %v", err)
	}
}

// formatIdx mirrors the cassette filename format used by OllamaRecorder.
func formatIdx(i int) string {
	return paddedInt(i, 3)
}

// paddedInt formats n left-padded with zeros to at least width digits.
func paddedInt(n, width int) string {
	s := ""
	for d := width - 1; d >= 0; d-- {
		digit := (n / pow10(d)) % 10
		s += string(rune('0' + digit))
	}
	return s
}

func pow10(n int) int {
	result := 1
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// TestOllamaRecorder_Replay verifies that the recorder correctly replays
// cassette fixtures in order without touching a real LLM.
func TestOllamaRecorder_Replay(t *testing.T) {
	cassetteDir := t.TempDir()
	cassetteDir = filepath.Join(cassetteDir, "fixtures")

	// Write two cassette turns.
	writeFixture(t, cassetteDir, "replay-test", 0, exec.AgentTurnResponse{
		Status: "tool_call", Tool: "proposal.create_draft", Args: `{"title":"T"}`,
	})
	writeFixture(t, cassetteDir, "replay-test", 1, exec.AgentTurnResponse{
		Status: "final", Content: "all done",
	})

	// Make sure RECORD_OLLAMA is not set so we go through the replay path.
	t.Setenv("RECORD_OLLAMA", "")

	recorder := exec.NewOllamaRecorder(cassetteDir, "replay-test", nil)
	agentFn := recorder.AgentFunc()

	// First call should return the tool_call turn.
	resp1, err := agentFn(context.Background(), exec.AgentTurnRequest{})
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if resp1.Status != "tool_call" {
		t.Errorf("call 1 status = %q, want tool_call", resp1.Status)
	}
	if resp1.Tool != "proposal.create_draft" {
		t.Errorf("call 1 tool = %q, want proposal.create_draft", resp1.Tool)
	}

	// Second call should return the final turn.
	resp2, err := agentFn(context.Background(), exec.AgentTurnRequest{})
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if resp2.Status != "final" {
		t.Errorf("call 2 status = %q, want final", resp2.Status)
	}
	if resp2.Content != "all done" {
		t.Errorf("call 2 content = %q, want 'all done'", resp2.Content)
	}
}

// TestOllamaRecorder_ReplayMissingCassette verifies that a helpful error is
// returned when the cassette file is absent.
func TestOllamaRecorder_ReplayMissingCassette(t *testing.T) {
	t.Setenv("RECORD_OLLAMA", "")
	cassetteDir := t.TempDir()
	recorder := exec.NewOllamaRecorder(cassetteDir, "missing-test", nil)
	agentFn := recorder.AgentFunc()

	_, err := agentFn(context.Background(), exec.AgentTurnRequest{})
	if err == nil {
		t.Fatal("expected error for missing cassette, got nil")
	}
	if !contains(err.Error(), "cassette not found") {
		t.Errorf("error should mention 'cassette not found': %v", err)
	}
	if !contains(err.Error(), "RECORD_OLLAMA=true") {
		t.Errorf("error should hint at RECORD_OLLAMA=true: %v", err)
	}
}

// TestOllamaRecorder_Record verifies that the recorder writes cassette files
// when RECORD_OLLAMA=true.
func TestOllamaRecorder_Record(t *testing.T) {
	t.Setenv("RECORD_OLLAMA", "true")
	cassetteDir := t.TempDir()

	calls := 0
	realAgentFn := func(_ context.Context, _ exec.AgentTurnRequest) (exec.AgentTurnResponse, error) {
		defer func() { calls++ }()
		if calls == 0 {
			return exec.AgentTurnResponse{Status: "tool_call", Tool: "t", Args: `{}`}, nil
		}
		return exec.AgentTurnResponse{Status: "final", Content: "recorded"}, nil
	}

	recorder := exec.NewOllamaRecorder(cassetteDir, "rec-test", realAgentFn)
	agentFn := recorder.AgentFunc()

	// Drive two calls.
	if _, err := agentFn(context.Background(), exec.AgentTurnRequest{}); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if _, err := agentFn(context.Background(), exec.AgentTurnRequest{}); err != nil {
		t.Fatalf("call 2: %v", err)
	}

	// Verify cassette files were written.
	for i := 0; i < 2; i++ {
		path := filepath.Join(cassetteDir, "rec-test-"+formatIdx(i)+".json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("cassette file %d not written: %s", i, path)
		}
	}
}

// TestOllamaRecorder_IntegratesWithReActRunner verifies that OllamaRecorder
// can be used as the AgentFunc of InProcessTaskExecutor and drive a
// ReActRunner through pre-recorded turns.
func TestOllamaRecorder_IntegratesWithReActRunner(t *testing.T) {
	cassetteDir := t.TempDir()
	cassetteDir = filepath.Join(cassetteDir, "react")

	// Write fixtures for a single tool-call + final-answer sequence.
	writeFixture(t, cassetteDir, "react-test", 0, exec.AgentTurnResponse{
		Status:   "tool_call",
		Tool:     "proposal.create_draft",
		Args:     `{"title":"Cassette Skill"}`,
		Thinking: "I will create the proposal.",
	})
	writeFixture(t, cassetteDir, "react-test", 1, exec.AgentTurnResponse{
		Status:  "final",
		Content: "Proposal created successfully.",
	})

	t.Setenv("RECORD_OLLAMA", "")

	recorder := exec.NewOllamaRecorder(cassetteDir, "react-test", nil)
	executor := exec.NewInProcessExecutor(recorder.AgentFunc())

	var transitions []exec.StateTransition
	runner := exec.NewReActRunner(
		executor,
		func(_ context.Context, _, _ string) (string, error) {
			return "proposal created with id abc123", nil
		},
		"create a new skill",
		exec.WithOnTransition(func(t exec.StateTransition) {
			transitions = append(transitions, t)
		}),
	)

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalAnswer != "Proposal created successfully." {
		t.Errorf("FinalAnswer = %q", result.FinalAnswer)
	}
	if result.ToolCallCount != 1 {
		t.Errorf("ToolCallCount = %d, want 1", result.ToolCallCount)
	}
	// Expect: Thinking→Acting, Acting→Observing, Observing→Thinking, Thinking→Finalizing
	if len(transitions) != 4 {
		t.Errorf("transitions = %d, want 4; got: %v", len(transitions), transitions)
	}
}
