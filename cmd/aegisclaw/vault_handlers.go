package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// All vault handlers are stubbed. The Host Daemon must never handle secrets
// (see docs/specs/host-daemon.md). Secret logic lives outside the TCB.

func makeVaultSecretAddHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		return &api.Response{Error: "vault disabled in minimal TCB (Phase 1)"}
	}
}

func makeVaultSecretListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		return &api.Response{Error: "vault disabled in minimal TCB (Phase 1)"}
	}
}

func makeVaultSecretGetHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		return &api.Response{Error: "vault disabled in minimal TCB (Phase 1)"}
	}
}

func makeVaultSecretDeleteHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		return &api.Response{Error: "vault disabled in minimal TCB (Phase 1)"}
	}
}

func makeVaultSecretRotateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env
		return &api.Response{Error: "vault disabled in minimal TCB (Phase 1)"}
	}
}
