package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"AegisClaw/internal/dashboard/sanitize"
)

// HandleChatSend serves POST /chat/send (JSON body, optional ?stream=1 SSE).
// The host daemon proxy uses this with a Hub-backed APIClient when the guest bridge is down.
func HandleChatSend(w http.ResponseWriter, r *http.Request, client APIClient) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10) // 512 KB limit
	var req struct {
		Input     string `json:"input"`
		SessionID string `json:"session_id,omitempty"`
		History   []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"history,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON: " + err.Error()}) //nolint:errcheck
		return
	}
	req.Input = strings.TrimSpace(req.Input)
	if req.Input == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "input required"}) //nolint:errcheck
		return
	}
	if r.URL.Query().Get("stream") == "1" || strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		streamID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
		payload := mustMarshal(map[string]interface{}{
			"input":      req.Input,
			"history":    req.History,
			"session_id": req.SessionID,
			"stream_id":  streamID,
		})
		handleChatSendStream(w, r, client, payload, streamID)
		return
	}
	payload := mustMarshal(map[string]interface{}{
		"input":      req.Input,
		"history":    req.History,
		"session_id": req.SessionID,
	})
	resp, err := client.Call(r.Context(), "chat.message", payload)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
		return
	}
	if resp == nil || !resp.Success {
		errMsg := "unknown error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		json.NewEncoder(w).Encode(map[string]string{"error": errMsg}) //nolint:errcheck
		return
	}
	w.Write(resp.Data) //nolint:errcheck
}

func handleChatSendStream(w http.ResponseWriter, r *http.Request, client APIClient, payload json.RawMessage, streamID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeSSE := func(v interface{}) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !writeSSE(map[string]interface{}{"type": "start", "ts": time.Now().UTC().Format(time.RFC3339)}) {
		return
	}

	ctx := r.Context()
	sessionID := sessionIDFromChatPayload(payload)
	lastToolID := latestEventID(ctx, client, "chat.tool_events", 60, sessionID)
	lastThoughtID := latestEventID(ctx, client, "chat.thought_events", 80, sessionID)
	emittedThinkingRunes := 0
	emittedContentRunes := 0
	lastProgressRequestID := ""

	type callResult struct {
		resp *APIResponse
		err  error
	}
	callDone := make(chan callResult, 1)
	go func() {
		resp, err := client.Call(ctx, "chat.message", payload)
		callDone <- callResult{resp: resp, err: err}
	}()

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()

	sendNewEvents := func() bool {
		toolEvents := fetchEventsSince(ctx, client, "chat.tool_events", 60, lastToolID, sessionID)
		for _, ev := range toolEvents {
			if id := eventID(ev); id > lastToolID {
				lastToolID = id
			}
			if !writeSSE(map[string]interface{}{"type": "tool_event", "event": sanitize.Value(sanitize.ContextTrace, ev)}) {
				return false
			}
		}
		if streamID != "" {
			progressReq := map[string]string{"stream_id": streamID}
			if sessionID != "" {
				progressReq["session_id"] = sessionID
			}
			progressRaw, err := fetchRaw(ctx, client, "chat.stream_progress", progressReq)
			if err == nil {
				if progress, ok := progressRaw.(map[string]interface{}); ok {
					requestID := toString(progress["request_id"])
					if requestID != "" && requestID != lastProgressRequestID {
						lastProgressRequestID = requestID
						emittedThinkingRunes = 0
						emittedContentRunes = 0
					}
					if !emitSnapshotDelta(writeSSE, "thought_delta", toString(progress["thinking"]), &emittedThinkingRunes) {
						return false
					}
					content := suppressInFlightStructuredContent(toString(progress["content"]))
					if !emitSnapshotDelta(writeSSE, "content_delta", content, &emittedContentRunes) {
						return false
					}
				}
			}
		}
		thoughtEvents := fetchEventsSince(ctx, client, "chat.thought_events", 80, lastThoughtID, sessionID)
		for _, ev := range thoughtEvents {
			if id := eventID(ev); id > lastThoughtID {
				lastThoughtID = id
			}
			if !writeSSE(map[string]interface{}{"type": "thought_event", "event": sanitize.Value(sanitize.ContextTrace, ev)}) {
				return false
			}
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !sendNewEvents() {
				return
			}
		case out := <-callDone:
			if !sendNewEvents() {
				return
			}
			if out.err != nil {
				writeSSE(map[string]interface{}{"type": "error", "error": out.err.Error()}) //nolint:errcheck
				return
			}
			if out.resp == nil || !out.resp.Success {
				errMsg := "unknown error"
				if out.resp != nil && out.resp.Error != "" {
					errMsg = out.resp.Error
				}
				writeSSE(map[string]interface{}{"type": "error", "error": errMsg}) //nolint:errcheck
				return
			}
			var data interface{}
			if len(out.resp.Data) > 0 {
				if err := json.Unmarshal(out.resp.Data, &data); err != nil {
					writeSSE(map[string]interface{}{"type": "error", "error": "invalid chat response JSON: " + err.Error()}) //nolint:errcheck
					return
				}
			}
			if m, ok := data.(map[string]interface{}); ok {
				if !emitSnapshotDelta(writeSSE, "content_delta", toString(m["content"]), &emittedContentRunes) {
					return
				}
			}
			writeSSE(map[string]interface{}{"type": "final", "data": data}) //nolint:errcheck
			return
		}
	}
}

func fetchRaw(ctx context.Context, client APIClient, action string, req interface{}) (interface{}, error) {
	if err := bridgeGuard.Validate(action); err != nil {
		return nil, err
	}
	var payload json.RawMessage
	if req != nil {
		payload, _ = json.Marshal(req)
	}
	resp, err := client.Call(ctx, action, payload)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response for action: %s", action)
	}
	if !resp.Success {
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		return nil, fmt.Errorf("action failed: %s", action)
	}
	var out interface{}
	json.Unmarshal(resp.Data, &out) //nolint:errcheck
	return out, nil
}

func sessionIDFromChatPayload(payload json.RawMessage) string {
	var m map[string]interface{}
	if json.Unmarshal(payload, &m) != nil {
		return ""
	}
	s, _ := m["session_id"].(string)
	return s
}

func pollPayload(limit int, sessionID string) map[string]interface{} {
	req := map[string]interface{}{"limit": limit}
	if sessionID != "" {
		req["session_id"] = sessionID
	}
	return req
}

func latestEventID(ctx context.Context, client APIClient, action string, limit int, sessionID string) int {
	raw, err := fetchRaw(ctx, client, action, pollPayload(limit, sessionID))
	if err != nil {
		return 0
	}
	items := toEventMaps(raw)
	maxID := 0
	for _, ev := range items {
		if id := eventID(ev); id > maxID {
			maxID = id
		}
	}
	return maxID
}

func fetchEventsSince(ctx context.Context, client APIClient, action string, limit int, lastID int, sessionID string) []map[string]interface{} {
	raw, err := fetchRaw(ctx, client, action, pollPayload(limit, sessionID))
	if err != nil {
		return nil
	}
	items := toEventMaps(raw)
	out := make([]map[string]interface{}, 0, len(items))
	for _, ev := range items {
		if eventID(ev) > lastID {
			out = append(out, ev)
		}
	}
	return out
}
