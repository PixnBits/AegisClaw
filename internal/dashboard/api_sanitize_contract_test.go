package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sanitizeMockClient struct{}

func (m *sanitizeMockClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	switch action {
	case "channel.get":
		data, _ := json.Marshal(map[string]interface{}{
			"id": "main",
			"messages": []interface{}{
				map[string]interface{}{
					"from":    "agent",
					"content": "read /etc/passwd api_key: sk-live-secretvalue",
				},
			},
		})
		return &APIResponse{Success: true, Data: data}, nil
	case "chat.tool_events":
		data, _ := json.Marshal([]interface{}{
			map[string]interface{}{
				"tool":   "read_file",
				"path":   "/etc/shadow",
				"input":  "token=abc123",
				"status": "success",
			},
		})
		return &APIResponse{Success: true, Data: data}, nil
	case "chat.thought_events":
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	default:
		return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
}

func TestChannelGetRedactsSensitiveContent(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &sanitizeMockClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/channels/main", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "/etc/passwd") || strings.Contains(body, "sk-live") {
		t.Fatalf("channel response leaked secrets: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("expected redaction markers: %s", body)
	}
}

func TestAgentTraceRedactsToolPaths(t *testing.T) {
	srv, _ := New("127.0.0.1:0", &sanitizeMockClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-1/trace", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "/etc/shadow") {
		t.Fatalf("trace leaked path: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("expected redaction in trace summary: %s", body)
	}
}