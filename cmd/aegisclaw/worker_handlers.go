package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// makeWorkerListHandler lists worker records.
func makeWorkerListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.WorkerStore == nil {
			return &api.Response{Error: "worker store not initialized"}
		}
		var req struct {
			ActiveOnly bool `json:"active_only"`
		}
		json.Unmarshal(data, &req) //nolint:errcheck

		workers := env.WorkerStore.List(req.ActiveOnly)
		out, err := json.Marshal(workers)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// makeWorkerStatusHandler returns a single worker record.
func makeWorkerStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.WorkerStore == nil {
			return &api.Response{Error: "worker store not initialized"}
		}
		var req struct {
			WorkerID string `json:"worker_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil || req.WorkerID == "" {
			return &api.Response{Error: "worker_id required"}
		}
		w, ok := env.WorkerStore.Get(req.WorkerID)
		if !ok {
			return &api.Response{Error: "worker " + req.WorkerID + " not found"}
		}
		out, err := json.Marshal(w)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// makeWorkerListActiveHandler is a convenience alias that returns only active workers.
func makeWorkerListActiveHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.WorkerStore == nil {
			return &api.Response{Error: "worker store not initialized"}
		}
		workers := env.WorkerStore.List(true)
		out, _ := json.Marshal(workers)
		return &api.Response{Success: true, Data: out}
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
