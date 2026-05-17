package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"go.uber.org/zap"
)

// makePRListHandler is stubbed — PR operations have moved out of the Host Daemon TCB.
func makePRListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makePRGetHandler is stubbed — PR operations have moved out of the Host Daemon TCB.
func makePRGetHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makePRApproveHandler is stubbed — PR operations have moved out of the Host Daemon TCB.
func makePRApproveHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makePRCloseHandler is stubbed — PR operations have moved out of the Host Daemon TCB.
func makePRCloseHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makePRMergeHandler is stubbed — PR operations have moved out of the Host Daemon TCB.
func makePRMergeHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}
