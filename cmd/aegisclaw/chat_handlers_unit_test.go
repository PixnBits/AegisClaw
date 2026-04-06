package main

import "testing"

func TestTraceContainsMemoryEvidence(t *testing.T) {
	toolCalls := []byte(`[
		{"tool":"retrieve_memory","success":true,"response":"memory_id=123 ttl_tier=90d"}
	]`)
	if !traceContainsMemoryEvidence(toolCalls) {
		t.Fatal("expected memory evidence in tool trace")
	}
}

func TestTraceContainsMemoryEvidence_NoEvidence(t *testing.T) {
	toolCalls := []byte(`[
		{"tool":"retrieve_memory","success":true,"response":"No memories found"}
	]`)
	if traceContainsMemoryEvidence(toolCalls) {
		t.Fatal("did not expect memory evidence when retrieval is empty")
	}
}
