package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// Composition handlers are thin shims in Phase 1.
// The Store interface is the only access path. Some methods (Rollback,
// History, UpdateHealth) are not yet on the interface and are stubbed here
// to keep the Host Daemon TCB minimal.

func makeCompositionCurrentHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "composition.current stubbed in Phase 1 TCB"}
	}
}

func makeCompositionGetHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "composition.get stubbed in Phase 1 TCB"}
	}
}

func makeCompositionPublishHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "composition.publish stubbed in Phase 1 TCB"}
	}
}

func makeCompositionRollbackHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "composition.rollback disabled in minimal TCB (Phase 1)"}
	}
}

func makeCompositionHistoryHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "composition.history disabled in minimal TCB (Phase 1)"}
	}
}

func makeCompositionHealthHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "composition.health disabled in minimal TCB (Phase 1)"}
	}
}
