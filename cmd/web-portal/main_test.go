package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestStreamMessage(t *testing.T) {
	msg := StreamMessage{
		Type:      "agent_response",
		MessageID: "msg_123",
		SessionID: "sess_456",
		Timestamp: "2026-05-10T00:00:00Z",
		TraceID:   "trace_789",
		Content:   map[string]interface{}{"text": "Hello", "is_complete": false},
		Metadata:  map[string]interface{}{"timing": "100ms"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var unmarshaled StreamMessage
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if unmarshaled.Type != "agent_response" {
		t.Errorf("Expected type agent_response, got %s", unmarshaled.Type)
	}
	if unmarshaled.Content["text"] != "Hello" {
		t.Errorf("Expected text Hello, got %v", unmarshaled.Content["text"])
	}
}

func TestSendSSE(t *testing.T) {
	// sendSSE is hard to test without HTTP, but we can check it doesn't panic
	msg := StreamMessage{
		Type:      "test",
		MessageID: "msg_test",
		SessionID: "sess_test",
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   "trace_test",
		Content:   map[string]interface{}{},
		Metadata:  map[string]interface{}{},
	}

	// Mock writer
	var output []byte
	writer := &mockWriter{output: &output}

	sendSSE(writer, msg)

	if len(output) == 0 {
		t.Error("Expected output from sendSSE")
	}
}

type mockWriter struct {
	output *[]byte
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	*m.output = append(*m.output, p...)
	return len(p), nil
}

func (m *mockWriter) Header() http.Header {
	return make(http.Header)
}

func (m *mockWriter) WriteHeader(int) {}

func TestExpandPath(t *testing.T) {
	path := expandPath("~/.aegis/test")
	if path == "~/.aegis/test" {
		t.Error("Expected path expansion")
	}
}