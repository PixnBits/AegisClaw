package llm

import (
	"strings"
	"testing"
)

// TestDecodeOllamaChatBodyToolCallsNotOverwritten ensures that native tool-calls
// from Ollama are not overwritten by later chunks with empty tool_calls.
// This is a regression test for the bug where tool_calls were being captured
// and then discarded when the final chunk arrived with empty content.
func TestDecodeOllamaChatBodyToolCallsNotOverwritten(t *testing.T) {
	// Simulate Ollama's streaming response where tool_calls come in first chunk
	// with empty content, and subsequent chunks have empty tool_calls and content.
	responseBody := `{"message":{"tool_calls":[{"function":{"name":"proposal.create_draft","arguments":"{\"title\":\"NOAA Weather\",\"skill_name\":\"noaa_weather\"}"}}],"content":""}}
{"message":{"content":"","tool_calls":[]}}
{"message":{"content":""}}`

	content, _, toolCalls, err := decodeOllamaChatBody(strings.NewReader(responseBody), nil)
	if err != nil {
		t.Fatalf("decodeOllamaChatBody failed: %v", err)
	}

	// Content should be empty
	if content != "" {
		t.Errorf("expected empty content, got: %q", content)
	}

	// Tool calls should NOT be empty or overwritten
	if len(toolCalls) == 0 {
		t.Fatal("expected tool_calls to be preserved, got empty slice")
	}

	if len(toolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(toolCalls))
	}

	toolCall := toolCalls[0]
	if toolCall.Function.Name != "proposal.create_draft" {
		t.Errorf("expected tool name 'proposal.create_draft', got %q", toolCall.Function.Name)
	}

	argumentsStr := string(toolCall.Function.Arguments)
	if !strings.Contains(argumentsStr, "NOAA Weather") {
		t.Errorf("expected arguments to contain 'NOAA Weather', got: %s", argumentsStr)
	}
}

// TestDecodeOllamaChatBodyContentWithToolCalls ensures content and tool-calls
// can coexist in a response without interference.
func TestDecodeOllamaChatBodyContentWithToolCalls(t *testing.T) {
	responseBody := `{"message":{"tool_calls":[{"function":{"name":"test_tool","arguments":"{\"param\":\"value\"}"}}],"content":"Some response text"}}
{"message":{"content":" continued"}}`

	content, _, toolCalls, err := decodeOllamaChatBody(strings.NewReader(responseBody), nil)
	if err != nil {
		t.Fatalf("decodeOllamaChatBody failed: %v", err)
	}

	// Both content and tool calls should be present
	expectedContent := "Some response text continued"
	if content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, content)
	}

	if len(toolCalls) == 0 {
		t.Fatal("expected tool_calls to be present")
	}

	if toolCalls[0].Function.Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got %q", toolCalls[0].Function.Name)
	}
}

// TestDecodeOllamaChatBodyMultipleToolCalls ensures multiple tool calls
// from the first chunk are captured.
func TestDecodeOllamaChatBodyMultipleToolCalls(t *testing.T) {
	responseBody := `{"message":{"tool_calls":[{"function":{"name":"tool1","arguments":"{}"}},{"function":{"name":"tool2","arguments":"{}"}}],"content":""}}
{"message":{"content":""}}`

	_, _, toolCalls, err := decodeOllamaChatBody(strings.NewReader(responseBody), nil)
	if err != nil {
		t.Fatalf("decodeOllamaChatBody failed: %v", err)
	}

	if len(toolCalls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(toolCalls))
	}

	if toolCalls[0].Function.Name != "tool1" {
		t.Errorf("expected first tool name 'tool1', got %q", toolCalls[0].Function.Name)
	}

	if toolCalls[1].Function.Name != "tool2" {
		t.Errorf("expected second tool name 'tool2', got %q", toolCalls[1].Function.Name)
	}
}

// TestDecodeOllamaChatBodyTokenizeReadingError ensures proper error handling
func TestDecodeOllamaChatBodyMalformedJSON(t *testing.T) {
	responseBody := `{"message":{"content":"valid"}}
{malformed json}`

	_, _, _, err := decodeOllamaChatBody(strings.NewReader(responseBody), nil)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// TestDecodeOllamaChatBodyEmptyResponse handles empty response gracefully
func TestDecodeOllamaChatBodyEmptyResponse(t *testing.T) {
	responseBody := ``

	content, _, toolCalls, err := decodeOllamaChatBody(strings.NewReader(responseBody), nil)
	// Empty response body is expected to return an error on first decode attempt
	if err == nil {
		t.Fatal("expected error for empty response")
	}

	if content != "" {
		t.Errorf("expected empty content, got: %q", content)
	}

	if len(toolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(toolCalls))
	}
}
