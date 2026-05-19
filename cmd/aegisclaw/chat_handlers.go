package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// Chat and session handlers are routed through ControlPlaneProxy (Phase 8).
// Per docs/specs/host-daemon.md the daemon must never process user messages
// or LLM output. All chat, sessions, tool events, and thought events are
// mediated via AegisHub. Real implementations live in AegisHub / Agent VMs.

func makeChatMessageHandler(proxy *ControlPlaneProxy) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if proxy == nil {
			return &api.Response{Error: "control plane proxy not available"}
		}
		resp, err := proxy.Forward(ctx, ControlPlaneRequest{
			Action: "chat.message",
			Data:   data,
		})
		if err != nil || !resp.Success {
			return &api.Response{Error: "chat.message via AegisHub failed"}
		}
		return &api.Response{Success: true, Data: resp.Data}
	}
}

