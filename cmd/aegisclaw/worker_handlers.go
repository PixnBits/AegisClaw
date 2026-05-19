package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// makeWorkerListHandler lists worker records.
// Phase 7: Routed via ControlPlaneProxy (AegisHub mediation).
// Long-term owner: Store VM via AegisHub.
func makeWorkerListHandler(env *runtimeEnv, proxy *ControlPlaneProxy) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if proxy == nil {
			return &api.Response{Error: "control plane proxy not available"}
		}
		resp, err := proxy.Forward(ctx, ControlPlaneRequest{
			Action: "worker.list",
			Data:   data,
		})
		if err != nil || !resp.Success {
			return &api.Response{Error: "worker list via AegisHub failed"}
		}
		return &api.Response{Success: true, Data: resp.Data}
	}
}

// makeWorkerStatusHandler returns a single worker record.
// Phase 7: Routed via ControlPlaneProxy (AegisHub mediation).
// Long-term owner: Store VM via AegisHub.
func makeWorkerStatusHandler(env *runtimeEnv, proxy *ControlPlaneProxy) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if proxy == nil {
			return &api.Response{Error: "control plane proxy not available"}
		}
		resp, err := proxy.Forward(ctx, ControlPlaneRequest{
			Action: "worker.status",
			Data:   data,
		})
		if err != nil || !resp.Success {
			return &api.Response{Error: "worker status via AegisHub failed"}
		}
		return &api.Response{Success: true, Data: resp.Data}
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
