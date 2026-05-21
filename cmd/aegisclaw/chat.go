package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// Chat orchestration has been removed from the Host Daemon in Phase 1.
// Per host-daemon.md the daemon must never process user messages or LLM output.
// All chat, memory injection, event bus timers, and ReAct loop logic now live
// in AegisHub and dedicated Agent Runtime VMs. This file is a stub to keep
// the package compiling while the TCB is minimized.

func makeChatSlashHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "chat.slash disabled in minimal TCB (Phase 1)"}
	}
}

func makeChatToolExecHandler(env *runtimeEnv, _ *ToolRegistry) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "chat.tool_exec disabled in minimal TCB (Phase 1)"}
	}
}

func makeChatSummarizeHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "chat.summarize disabled in minimal TCB (Phase 1)"}
	}
}
