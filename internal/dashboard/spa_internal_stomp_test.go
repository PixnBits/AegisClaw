package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"AegisClaw/internal/portalstomp"
)

func TestHandleInternalChannelActivitySTOMPLoopbackOnly(t *testing.T) {
	s := &Server{stompHub: portalstomp.NewHub()}
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
}
