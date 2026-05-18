package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// Chat and session handlers are stubbed in Phase 1 Minimal TCB.
// Per docs/specs/host-daemon.md the daemon must never process user messages
// or LLM output. All chat, sessions, tool events, and thought events have
// been removed from the Host Daemon. Real implementations live in AegisHub
// and Agent Runtime VMs.

func makeChatMessageHandler(env *runtimeEnv, toolRegistry *ToolRegistry) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = toolRegistry
		_ = data
		return &api.Response{Error: "chat.message disabled in minimal TCB daemon (Phase 1)"}
	}
}

