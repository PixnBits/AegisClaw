package main

import (
	"strings"
	"testing"
)

// Tests for synthesizeEmptyFinalMessage (Improvement 2).
// These are pure unit tests that do not require Ollama, KVM, or cassettes.

// Test A: last tool is proposal.vote with success=true → vote-aware message.
func TestSynthesizeEmptyFinalMessage_VoteSuccess(t *testing.T) {
	trace := []map[string]interface{}{
		{"tool": "proposal.create_draft", "success": true},
		{"tool": "proposal.vote", "success": true},
	}
	got := synthesizeEmptyFinalMessage(trace)
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "vote") && !strings.Contains(lower, "proposal") {
		t.Errorf("expected vote-aware message containing 'vote' or 'proposal', got: %q", got)
	}
	if strings.Contains(lower, "final response came back empty") {
		t.Errorf("expected vote-specific message, got generic fallback: %q", got)
	}
}

// Test B: last tool is proposal.vote with success=false → failure mention.
func TestSynthesizeEmptyFinalMessage_VoteFailure(t *testing.T) {
	trace := []map[string]interface{}{
		{"tool": "proposal.vote", "success": false},
	}
	got := synthesizeEmptyFinalMessage(trace)
	if got == "" {
		t.Error("expected non-empty message for failed vote")
	}
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "could not") && !strings.Contains(lower, "fail") && !strings.Contains(lower, "try again") {
		t.Errorf("expected failure/retry message for failed vote, got: %q", got)
	}
}

// Test C: empty toolTrace → generic "please ask again" fallback.
func TestSynthesizeEmptyFinalMessage_EmptyTrace(t *testing.T) {
	got := synthesizeEmptyFinalMessage(nil)
	if got == "" {
		t.Error("expected non-empty message for nil trace")
	}
	got2 := synthesizeEmptyFinalMessage([]map[string]interface{}{})
	if got2 == "" {
		t.Error("expected non-empty message for empty trace")
	}
}

// Test D: last tool is a non-vote tool → generic fallback, no regression.
func TestSynthesizeEmptyFinalMessage_NonVoteTool(t *testing.T) {
	traceSuccess := []map[string]interface{}{
		{"tool": "activate_skill", "success": true},
	}
	gotSuccess := synthesizeEmptyFinalMessage(traceSuccess)
	if !strings.Contains(gotSuccess, "activate_skill") {
		t.Errorf("expected tool name in generic fallback, got: %q", gotSuccess)
	}
	if strings.Contains(strings.ToLower(gotSuccess), "vote") {
		t.Errorf("non-vote tool should not produce vote-specific message, got: %q", gotSuccess)
	}

	traceFailure := []map[string]interface{}{
		{"tool": "activate_skill", "success": false},
	}
	gotFailure := synthesizeEmptyFinalMessage(traceFailure)
	if !strings.Contains(gotFailure, "activate_skill") {
		t.Errorf("expected tool name in failure fallback, got: %q", gotFailure)
	}
}
