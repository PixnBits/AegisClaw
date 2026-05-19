package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// makeWorkerListHandler lists worker records.
// Phase 5: WorkerStore access removed from Host Daemon TCB.
// Long-term owner: Store VM. Access routed through AegisHub mediation.
func makeWorkerListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "worker store access removed from minimal Host Daemon TCB (Phase 5)"}
	}
}

// makeWorkerStatusHandler returns a single worker record.
// Phase 5: WorkerStore access removed from Host Daemon TCB.
// Long-term owner: Store VM. Access routed through AegisHub mediation.
func makeWorkerStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		_ = env
		_ = data
		return &api.Response{Error: "worker store access removed from minimal Host Daemon TCB (Phase 5)"}
	}
}

// makeWorkerListActiveHandler is a convenience alias that returns only active workers.
// Phase 5: WorkerStore access removed from Host Daemon TCB.
// Long-term owner: Store VM. Access routed through AegisHub mediation.
func makeWorkerListActiveHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		_ = env
		return &api.Response{Error: "worker store access removed from minimal Host Daemon TCB (Phase 5)"}
	}
}

// workerListItem is a compact summary row for list views.
type workerListItem struct {
	WorkerID        string              `json:"worker_id"`
	Role            worker.Role         `json:"role"`
	Status          worker.WorkerStatus `json:"status"`
	StepCount       int                 `json:"step_count"`
	TaskID          string              `json:"task_id"`
	SpawnedAt       string              `json:"spawned_at"`
	TaskDescription string              `json:"task_description"`
}
