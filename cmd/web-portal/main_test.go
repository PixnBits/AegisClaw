package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStreamMessageRoundTrip(t *testing.T) {
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
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if unmarshaled.Type != "agent_response" {
		t.Fatalf("Expected type agent_response, got %s", unmarshaled.Type)
	}
	if unmarshaled.Content["text"] != "Hello" {
		t.Fatalf("Expected text Hello, got %v", unmarshaled.Content["text"])
	}
}

func TestSendSSE(t *testing.T) {
	msg := StreamMessage{
		Type:      "test",
		MessageID: "msg_test",
		SessionID: "sess_test",
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   "trace_test",
		Content:   map[string]interface{}{},
		Metadata:  map[string]interface{}{},
	}

	var output []byte
	writer := &mockWriter{output: &output}
	sendSSE(writer, msg)

	if got := string(output); !strings.HasPrefix(got, "data: {") || !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("unexpected SSE payload: %q", got)
	}
}

func TestExpandPath(t *testing.T) {
	path := expandPath("~/.aegis/test")
	if path == "~/.aegis/test" {
		t.Fatal("expected path expansion")
	}
}

func TestIsSafeSessionID(t *testing.T) {
	if !isSafeSessionID("sess_123-abc") {
		t.Fatal("expected session id to be valid")
	}
	if isSafeSessionID("../../bad") {
		t.Fatal("expected traversal-like session id to be rejected")
	}
}

func TestIncrementalChunks(t *testing.T) {
	chunks := incrementalChunks("one two three")
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[2] != "one two three" {
		t.Fatalf("unexpected final chunk: %q", chunks[2])
	}
}

func TestMuxServesEmbeddedIndexWithSecurityHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("expected CSP header, got %q", csp)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Fatalf("expected Dashboard heading in index")
	}
	if strings.Contains(body, "cdn.jsdelivr.net") {
		t.Fatal("external CDN reference should not be present")
	}
}

func TestSPAFallbackServesIndex(t *testing.T) {
	for _, path := range []string{"/dashboard", "/skills", "/court", "/monitoring"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		newMux().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for SPA path %s, got %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "AegisClaw") {
			t.Fatalf("expected index.html for SPA path %s", path)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected json content type, got %q", got)
	}
}

func TestBackendDataEndpoints(t *testing.T) {
	dir := t.TempDir()
	writeTestStoreJSON(t, filepath.Join(dir, "skills.json"), `{"discord_monitor":{"id":"discord_monitor","name":"Discord Monitor","version":"1.2","status":"Deployed","description":"Monitors Discord","required_scopes":["network:discord.com"],"secrets":["DISCORD_BOT_TOKEN"]}}`)
	writeTestStoreJSON(t, filepath.Join(dir, "proposals.json"), `{"prop1":{"id":"prop1","description":"Add new skill","state":"pending","reviews":{"ciso":"approve"}}}`)
	t.Setenv("AEGIS_STORE_DATA_DIR", dir)

	for _, endpoint := range []string{"/api/dashboard", "/api/skills", "/api/proposals", "/api/monitoring"} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		rec := httptest.NewRecorder()
		newMux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", endpoint, rec.Code)
		}
	}

	skillsReq := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	skillsRec := httptest.NewRecorder()
	newMux().ServeHTTP(skillsRec, skillsReq)
	if !strings.Contains(skillsRec.Body.String(), "Discord Monitor") {
		t.Fatalf("expected backend skill data in response, got %s", skillsRec.Body.String())
	}
}

func TestChatStreamRequiresInputs(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/chat/stream", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChatStreamEmitsExpectedRailSequence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{Response: "Ollama test response"})
	}))
	defer server.Close()
	t.Setenv("AEGIS_OLLAMA_URL", server.URL)

	reset := setTestStreamingDelays()
	defer reset()

	req := httptest.NewRequest(http.MethodGet, "/api/chat/stream?message=hello%20portal&session_id=sess_123", nil)
	rec := httptest.NewRecorder()

	handleChatStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	events := decodeSSEEvents(t, rec.Body.Bytes())
	if len(events) < 5 {
		t.Fatalf("expected at least 5 events, got %d", len(events))
	}

	expectedPrefix := []string{"user_message", "agent_thinking", "tool_call", "tool_result", "agent_response"}
	for i, want := range expectedPrefix {
		if events[i].Type != want {
			t.Fatalf("event %d: expected %s, got %s", i, want, events[i].Type)
		}
	}

	last := events[len(events)-1]
	if last.Type != "agent_response" || last.Content["is_complete"] != true {
		t.Fatalf("expected final complete agent_response")
	}
	if !strings.Contains(last.Content["text"].(string), "Ollama") {
		t.Fatalf("expected Ollama-backed response text, got %q", last.Content["text"])
	}
}

func decodeSSEEvents(t *testing.T, payload []byte) []StreamMessage {
	t.Helper()

	scanner := bufio.NewScanner(bytes.NewReader(payload))
	var events []StreamMessage
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event StreamMessage
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("failed to decode SSE event: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	return events
}

func setTestStreamingDelays() func() {
	originalInitial := initialResponseDelay
	originalThinking := thinkingDelay
	originalTool := toolResultDelay
	originalFinal := finalResponseDelay
	originalWord := wordStreamDelay

	initialResponseDelay = 0
	thinkingDelay = 0
	toolResultDelay = 0
	finalResponseDelay = 0
	wordStreamDelay = 0

	return func() {
		initialResponseDelay = originalInitial
		thinkingDelay = originalThinking
		toolResultDelay = originalTool
		finalResponseDelay = originalFinal
		wordStreamDelay = originalWord
	}
}

func writeTestStoreJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
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
