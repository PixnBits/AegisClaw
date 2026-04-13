package main

import (
	"encoding/json"
	"testing"
)

func TestDecodeStructuredChatResponseNativeToolCall(t *testing.T) {
	toolCalls := []proxyToolCall{{}}
	toolCalls[0].Function.Name = "proposal.create_draft"
	toolCalls[0].Function.Arguments = json.RawMessage(`{"title":"NOAA Weather Integration"}`)

	resp, ok := decodeStructuredChatResponse("", "thinking", toolCalls, false)
	if !ok {
		t.Fatal("expected native tool call to be accepted")
	}
	if resp.Status != "tool_call" {
		t.Fatalf("expected tool_call status, got %q", resp.Status)
	}
	if resp.Tool != "proposal.create_draft" {
		t.Fatalf("expected proposal.create_draft, got %q", resp.Tool)
	}
	if resp.Args != `{"title":"NOAA Weather Integration"}` {
		t.Fatalf("unexpected args: %s", resp.Args)
	}
}

func TestDecodeStructuredChatResponseStructuredJSON(t *testing.T) {
	resp, ok := decodeStructuredChatResponse(`{"status":"final","content":"done"}`, "thinking", nil, false)
	if !ok {
		t.Fatal("expected structured JSON response to parse")
	}
	if resp.Status != "final" || resp.Content != "done" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestDecodeStructuredChatResponseFencedJSON(t *testing.T) {
	resp, ok := decodeStructuredChatResponse("```json\n{\"status\":\"final\",\"content\":\"done\"}\n```", "thinking", nil, false)
	if !ok {
		t.Fatal("expected fenced JSON response to parse")
	}
	if resp.Status != "final" || resp.Content != "done" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestDecodeStructuredChatResponseToolCallMarkdownFallback(t *testing.T) {
	resp, ok := decodeStructuredChatResponse("```tool-call\n{\"name\":\"proposal.submit\",\"args\":{\"id\":\"abc\"}}\n```", "thinking", nil, false)
	if !ok {
		t.Fatal("expected tool-call markdown fallback to parse")
	}
	if resp.Status != "tool_call" || resp.Tool != "proposal.submit" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Args != `{"id":"abc"}` {
		t.Fatalf("unexpected args: %s", resp.Args)
	}
}

func TestDecodeStructuredChatResponsePlainFinalOnlyOnLastAttempt(t *testing.T) {
	if _, ok := decodeStructuredChatResponse("plain text reply", "thinking", nil, false); ok {
		t.Fatal("did not expect plain text to be accepted before final attempt")
	}

	resp, ok := decodeStructuredChatResponse("plain text reply", "thinking", nil, true)
	if !ok {
		t.Fatal("expected plain text to be accepted on final attempt")
	}
	if resp.Status != "final" || resp.Content != "plain text reply" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
