package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// makeCompositionCurrentHandler returns the current composition manifest (D10).
func makeCompositionCurrentHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.CompositionStore == nil {
			return &api.Response{Error: "composition store not initialized"}
		}

		current := env.CompositionStore.Current()
		if current == nil {
			return &api.Response{Success: true, Data: json.RawMessage(`{"version":0}`)}
		}

		respData, err := json.Marshal(current)
		if err != nil {
			return &api.Response{Error: "failed to marshal manifest: " + err.Error()}
		}
		return &api.Response{Success: true, Data: respData}
	}
}

// compositionRollbackRequest carries the payload for the rollback action.
type compositionRollbackRequest struct {
	TargetVersion int    `json:"target_version,omitempty"`
	Reason        string `json:"reason"`
}

// makeCompositionRollbackHandler rolls back to a previous composition version (D10).
// If target_version is 0, it rolls back to the immediately previous version.
func makeCompositionRollbackHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.CompositionStore == nil {
			return &api.Response{Error: "composition store not initialized"}
		}

		var req compositionRollbackRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Reason == "" {
			req.Reason = "operator-initiated rollback"
		}

		var m *composition.Manifest
		var err error

		if req.TargetVersion > 0 {
			m, err = env.CompositionStore.Rollback(req.TargetVersion, "daemon", req.Reason)
		} else {
			m, err = env.CompositionStore.RollbackToPrevious("daemon", req.Reason)
		}

		if err != nil {
			return &api.Response{Error: "rollback failed: " + err.Error()}
		}

		// Audit log the rollback.
		payload, _ := json.Marshal(map[string]interface{}{
			"from_version": m.Version - 1,
			"to_version":   m.Version,
			"reason":       req.Reason,
			"components":   len(m.Components),
		})
		action := kernel.NewAction(kernel.ActionCompositionRollback, "daemon", payload)
		env.Kernel.SignAndLog(action)

		env.Logger.Info("composition rollback complete",
			zap.Int("new_version", m.Version),
			zap.String("reason", req.Reason),
		)

		respData, _ := json.Marshal(m)
		return &api.Response{Success: true, Data: respData}
	}
}

// makeCompositionHistoryHandler returns the version history (D10).
func makeCompositionHistoryHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.CompositionStore == nil {
			return &api.Response{Error: "composition store not initialized"}
		}

		history, err := env.CompositionStore.History()
		if err != nil {
			return &api.Response{Error: "failed to load history: " + err.Error()}
		}

		type historySummary struct {
			Version   int    `json:"version"`
			Hash      string `json:"hash"`
			CreatedAt string `json:"created_at"`
			CreatedBy string `json:"created_by"`
			Reason    string `json:"reason"`
			Components int   `json:"components"`
		}

		summaries := make([]historySummary, len(history))
		for i, m := range history {
			summaries[i] = historySummary{
				Version:    m.Version,
				Hash:       m.Hash[:16],
				CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05Z"),
				CreatedBy:  m.CreatedBy,
				Reason:     m.Reason,
				Components: len(m.Components),
			}
		}

		respData, _ := json.Marshal(summaries)
		return &api.Response{Success: true, Data: respData}
	}
}

// compositionHealthRequest carries the payload for the health update action.
type compositionHealthRequest struct {
	Component string `json:"component"`
	Status    string `json:"status"`
}

// makeCompositionHealthHandler updates component health and triggers
// automatic rollback if any component becomes unhealthy (D10).
func makeCompositionHealthHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.CompositionStore == nil {
			return &api.Response{Error: "composition store not initialized"}
		}

		var req compositionHealthRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Component == "" {
			return &api.Response{Error: "component name is required"}
		}

		health := composition.HealthStatus(req.Status)
		switch health {
		case composition.HealthHealthy, composition.HealthDegraded, composition.HealthUnhealthy:
		default:
			return &api.Response{Error: fmt.Sprintf("invalid health status: %q", req.Status)}
		}

		if err := env.CompositionStore.UpdateHealth(req.Component, health); err != nil {
			return &api.Response{Error: "failed to update health: " + err.Error()}
		}

		// D10: Automatic rollback on unhealthy component.
		if health == composition.HealthUnhealthy && env.CompositionStore.NeedsRollback() {
			env.Logger.Warn("unhealthy component detected, attempting automatic rollback",
				zap.String("component", req.Component),
			)

			m, err := env.CompositionStore.RollbackToPrevious("auto-health", fmt.Sprintf("component %q unhealthy", req.Component))
			if err != nil {
				env.Logger.Error("automatic rollback failed", zap.Error(err))
				return &api.Response{Error: "health updated but automatic rollback failed: " + err.Error()}
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"trigger":     "health_failure",
				"component":   req.Component,
				"new_version": m.Version,
			})
			action := kernel.NewAction(kernel.ActionCompositionRollback, "auto-health", payload)
			env.Kernel.SignAndLog(action)

			respData, _ := json.Marshal(map[string]interface{}{
				"health_updated":     true,
				"automatic_rollback": true,
				"new_version":        m.Version,
			})
			return &api.Response{Success: true, Data: respData}
		}

		return &api.Response{Success: true}
	}
}
