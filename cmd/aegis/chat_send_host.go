package main

import (
	"context"
	"encoding/json"
	"net/http"

	"AegisClaw/internal/dashboard"
)

// hostChatAPIClient routes dashboard chat actions through the host Hub / portal bridge
// when the web-portal guest cannot reach vsock :1030 (same pattern as /api/chat/*).
type hostChatAPIClient struct{}

func (hostChatAPIClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	var raw interface{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &raw)
	}
	var (
		resp interface{}
		err  error
	)
	if action == "chat.message" {
		resp, err = handlePortalChatAction(action, raw)
	} else {
		resp, err = handlePortalBridgeAction(action, raw)
	}
	if err != nil {
		return &dashboard.APIResponse{Success: false, Error: err.Error()}, nil
	}
	data, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return &dashboard.APIResponse{Success: false, Error: marshalErr.Error()}, nil
	}
	return &dashboard.APIResponse{Success: true, Data: data}, nil
}

func handleHostChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	client := hostChatAPIClient{}
	dashboard.HandleChatSend(w, r, client)
}
