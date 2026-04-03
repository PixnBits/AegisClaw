package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
)

// makeApprovalsListHandler lists approval requests (all or pending-only).
func makeApprovalsListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.EventBus == nil {
			return &api.Response{Error: "event bus not initialized"}
		}
		var req struct {
			PendingOnly bool `json:"pending_only"`
		}
		json.Unmarshal(data, &req) //nolint:errcheck
		var approvals []*eventbus.ApprovalRequest
		if req.PendingOnly {
			approvals = env.EventBus.ListPendingApprovals()
		} else {
			approvals = env.EventBus.ListApprovals()
		}
		out, err := json.Marshal(approvals)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// approvalsDecideRequest carries the payload for the decide action.
type approvalsDecideRequest struct {
	ApprovalID string `json:"approval_id"`
	Approved   bool   `json:"approved"`
	DecidedBy  string `json:"decided_by"`
	Reason     string `json:"reason"`
}

// makeApprovalsDecideHandler renders a human approve/reject decision.
func makeApprovalsDecideHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.EventBus == nil {
			return &api.Response{Error: "event bus not initialized"}
		}
		var req approvalsDecideRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.ApprovalID == "" {
			return &api.Response{Error: "approval_id required"}
		}
		if req.DecidedBy == "" {
			req.DecidedBy = "user"
		}
		if err := env.EventBus.DecideApproval(req.ApprovalID, req.Approved, req.DecidedBy, req.Reason); err != nil {
			return &api.Response{Error: err.Error()}
		}
		// Merkle-audit the decision.
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"approval_id": req.ApprovalID, "approved": req.Approved, "decided_by": req.DecidedBy,
		})
		act := kernel.NewAction(kernel.ActionApprovalDecide, req.DecidedBy, auditPayload)
		env.Kernel.SignAndLog(act) //nolint:errcheck

		decision := "rejected"
		if req.Approved {
			decision = "approved"
		}
		return &api.Response{
			Success: true,
			Data:    json.RawMessage(fmt.Sprintf(`{"approval_id":%q,"decision":%q}`, req.ApprovalID, decision)),
		}
	}
}

// makeTimersListHandler lists active timers.
func makeTimersListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.EventBus == nil {
			return &api.Response{Error: "event bus not initialized"}
		}
		var req struct {
			Status string `json:"status"` // "" = all
		}
		json.Unmarshal(data, &req) //nolint:errcheck
		timers := env.EventBus.ListTimers(eventbus.TimerStatus(req.Status))
		out, err := json.Marshal(timers)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// makeSignalsListHandler lists received signals.
func makeSignalsListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.EventBus == nil {
			return &api.Response{Error: "event bus not initialized"}
		}
		var req struct {
			TaskID string `json:"task_id"`
			Limit  int    `json:"limit"`
		}
		json.Unmarshal(data, &req) //nolint:errcheck
		if req.Limit <= 0 {
			req.Limit = 50
		}
		signals := env.EventBus.ListSignals(req.TaskID, req.Limit)
		out, err := json.Marshal(signals)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// formatTimerRow formats a single timer for text display.
func formatTimerRow(t *eventbus.Timer) string {
	next := "N/A"
	if t.NextFireAt != nil {
		next = t.NextFireAt.Format(time.RFC3339)
	} else if t.TriggerAt != nil {
		next = t.TriggerAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("  [%s]  %-24s  %-10s  next=%-25s  task=%s",
		t.TimerID[:8], t.Name, t.Status, next, t.TaskID)
}
