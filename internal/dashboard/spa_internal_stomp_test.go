package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"AegisClaw/internal/portalstomp"
)

type noopAPIClient struct{}

func (noopAPIClient) Call(context.Context, string, json.RawMessage) (*APIResponse, error) {
	return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
}

func TestHandleInternalChannelActivitySTOMPLoopbackOnly(t *testing.T) {
	s := &Server{
		stompHub:  portalstomp.NewHub(),
		apiClient: noopAPIClient{},
	}
	body, _ := json.Marshal(map[string]string{
		"channel_id": "main",
		"from":       "project-manager-main",
		"content":    "hello",
	})

	req := httptest.NewRequest(http.MethodPost, "/internal/realtime/channel-activity", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	s.handleInternalChannelActivitySTOMP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("loopback POST: got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/internal/realtime/channel-activity", bytes.NewReader(body))
	req2.RemoteAddr = "10.0.0.1:12345"
	w2 := httptest.NewRecorder()
	s.handleInternalChannelActivitySTOMP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("remote POST: got %d, want 403", w2.Code)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/internal/realtime/channel-activity", bytes.NewReader(body))
	req3.RemoteAddr = "10.0.0.1:12345"
	req3.Header.Set(ChannelNotifyHeader, "1")
	w3 := httptest.NewRecorder()
	s.handleInternalChannelActivitySTOMP(w3, req3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("daemon notify POST: got %d", w3.Code)
	}
}
