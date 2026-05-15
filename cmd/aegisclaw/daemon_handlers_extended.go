package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"go.uber.org/zap"
)

// registerExtendedDaemonAPI wires additional JSON-RPC-style handlers used by
// the expanded CLI surface (docs/specs/cli.md). Handlers stay thin: validate
// input, touch only host-side registries, and return structured JSON.
func registerExtendedDaemonAPI(
	apiSrv *api.Server,
	env *runtimeEnv,
	toolRegistry *ToolRegistry,
	hub *ipc.MessageHub,
	daemonQuit chan struct{},
) {
	apiSrv.Handle("vault.secret.add", makeVaultSecretAddHandler(env))
	apiSrv.Handle("vault.secret.list", makeVaultSecretListHandler(env))
	apiSrv.Handle("vault.secret.delete", makeVaultSecretDeleteHandler(env))
	apiSrv.Handle("vault.secret.rotate", makeVaultSecretRotateHandler(env))

	apiSrv.Handle("worker.list", makeWorkerListHandler(env))
	apiSrv.Handle("worker.status", makeWorkerStatusHandler(env))

	apiSrv.Handle("skill.list", makeSkillListHandler(env))
	apiSrv.Handle("skill.status", makeSkillStatusHandler(env))
	apiSrv.Handle("skill.deactivate", makeSkillDeactivateHandler(env))
	apiSrv.Handle("skill.activate", makeSkillActivateHandler(env))
	apiSrv.Handle("skill.secrets.refresh", makeSkillSecretsRefreshHandler(env))

	apiSrv.Handle("chat.message", makeChatMessageHandler(env, toolRegistry))
	apiSrv.Handle("chat.slash", makeChatSlashHandler(env))
	apiSrv.Handle("chat.tool", makeChatToolExecHandler(env, toolRegistry))
	apiSrv.Handle("chat.summarize", makeChatSummarizeHandler(env))

	apiSrv.Handle("kernel.shutdown", makeKernelShutdownHandler(env, hub, apiSrv, daemonQuit))

	apiSrv.Handle("sessions.list", makeSessionsListHandler(env))
	apiSrv.Handle("sessions.history", makeSessionsHistoryHandler(env))
	apiSrv.Handle("sessions.send", makeSessionsSendHandler(env, toolRegistry))
	apiSrv.Handle("sessions.spawn", makeSessionsSpawnHandler(env, toolRegistry))
	apiSrv.Handle("sessions.status", makeSessionsStatusHandler(env))
	apiSrv.Handle("sessions.pause", makeSessionsPauseHandler(env))
	apiSrv.Handle("sessions.resume", makeSessionsResumeHandler(env))
	apiSrv.Handle("sessions.cancel", makeSessionsCancelHandler(env))

	apiSrv.Handle("tasks.list", makeTasksListHandler(env))
	apiSrv.Handle("tasks.status", makeTasksStatusHandler(env))
	apiSrv.Handle("tasks.pause", makeTasksPauseStubHandler())
	apiSrv.Handle("tasks.resume", makeTasksResumeStubHandler())
	apiSrv.Handle("tasks.cancel", makeTasksCancelStubHandler())

	apiSrv.Handle("court.decisions.list", makeCourtDecisionsListHandler(env))
	apiSrv.Handle("court.decisions.show", makeCourtDecisionsShowHandler(env))

	if env.TeamRegistry != nil {
		apiSrv.Handle("team.list", makeTeamListHandler(env))
		apiSrv.Handle("team.create", makeTeamCreateHandler(env))
		apiSrv.Handle("team.join", makeTeamJoinHandler(env))
		apiSrv.Handle("team.leave", makeTeamLeaveHandler(env))
		apiSrv.Handle("team.status", makeTeamStatusHandler(env))
	}
	if env.AutonomyRegistry != nil {
		apiSrv.Handle("autonomy.show", makeAutonomyShowHandler(env))
		apiSrv.Handle("autonomy.grant", makeAutonomyGrantHandler(env))
		apiSrv.Handle("autonomy.revoke", makeAutonomyRevokeHandler(env))
		apiSrv.Handle("autonomy.reset", makeAutonomyResetHandler(env))
	}
}

// makeUnimplementedHandler returns a registered handler that fails with an
// explicit message (never "unknown action"). Used for TDD: contract tests
// expect implStub until replaced with a real implementation.
func makeUnimplementedHandler(action string) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: action + " not implemented in this build context"}
	}
}

func makeSkillActivateHandler(env *runtimeEnv) api.Handler {
	_ = env
	return makeUnimplementedHandler("skill.activate")
}

func makeSkillSecretsRefreshHandler(env *runtimeEnv) api.Handler {
	_ = env
	return makeUnimplementedHandler("skill.secrets.refresh")
}

func makeSkillListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.Registry == nil {
			return &api.Response{Error: "skill registry not initialized"}
		}
		skills := env.Registry.List()
		out, err := json.Marshal(skills)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

func makeSkillStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			return &api.Response{Error: "name is required"}
		}
		if env.Registry == nil {
			return &api.Response{Error: "skill registry not initialized"}
		}
		entry, ok := env.Registry.Get(req.Name)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("skill %q not found", req.Name)}
		}
		out, err := json.Marshal(entry)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

func makeSkillDeactivateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.SkillDeactivateRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			return &api.Response{Error: "name is required"}
		}
		if env.Registry == nil {
			return &api.Response{Error: "skill registry not initialized"}
		}
		entry, ok := env.Registry.Get(req.Name)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("skill %q not found", req.Name)}
		}
		if entry.State == sandbox.SkillStateActive && entry.SandboxID != "" && env.Runtime != nil {
			_ = env.Runtime.Stop(ctx, entry.SandboxID)
			_ = env.Runtime.Delete(ctx, entry.SandboxID)
		}
		if err := env.Registry.Deactivate(req.Name); err != nil {
			return &api.Response{Error: err.Error()}
		}
		payload, _ := json.Marshal(map[string]string{"skill_name": req.Name})
		action := kernel.NewAction(kernel.ActionSkillDeactivate, "api", payload)
		if _, err := env.Kernel.SignAndLog(action); err != nil {
			env.Logger.Error("failed to audit log skill deactivate", zap.Error(err))
		}
		return &api.Response{Success: true, Data: mustMarshal(map[string]string{"message": "deactivated " + req.Name})}
	}
}

func makeKernelShutdownHandler(env *runtimeEnv, hub *ipc.MessageHub, apiSrv *api.Server, daemonQuit chan struct{}) api.Handler {
	var once sync.Once
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		once.Do(func() {
			payload, _ := json.Marshal(map[string]string{"reason": "kernel.shutdown"})
			action := kernel.NewAction(kernel.ActionKernelStop, "cli", payload)
			if _, err := env.Kernel.SignAndLog(action); err != nil {
				env.Logger.Error("failed to log kernel shutdown", zap.Error(err))
			}
			if hub != nil {
				hub.Stop()
			}
			if apiSrv != nil {
				apiSrv.Stop()
			}
			if daemonQuit != nil {
				close(daemonQuit)
			}
		})
		return &api.Response{Success: true, Data: mustMarshal(map[string]string{"message": "shutdown initiated"})}
	}
}

func makeSessionsStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		rec, ok := env.Sessions.Get(req.SessionID)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		msgs, _ := env.Sessions.History(req.SessionID, 0)
		out, err := json.Marshal(map[string]interface{}{
			"session_id":     rec.ID,
			"sandbox_id":     rec.SandboxID,
			"status":         rec.Status,
			"started_at":     rec.StartedAt.UTC().Format(time.RFC3339),
			"last_active_at": rec.LastActiveAt.UTC().Format(time.RFC3339),
			"message_count":  len(msgs),
		})
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

func makeSessionsPauseHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		if _, ok := env.Sessions.Get(req.SessionID); !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		env.Sessions.SetStatus(req.SessionID, sessions.StatusPaused)
		return &api.Response{Success: true, Data: mustMarshal(map[string]string{"status": string(sessions.StatusPaused)})}
	}
}

func makeSessionsResumeHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		if _, ok := env.Sessions.Get(req.SessionID); !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		env.Sessions.SetStatus(req.SessionID, sessions.StatusIdle)
		return &api.Response{Success: true, Data: mustMarshal(map[string]string{"status": string(sessions.StatusIdle)})}
	}
}

func makeSessionsCancelHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		if _, ok := env.Sessions.Get(req.SessionID); !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		env.Sessions.Close(req.SessionID)
		return &api.Response{Success: true, Data: mustMarshal(map[string]string{"status": string(sessions.StatusClosed)})}
	}
}

func makeTasksListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.WorkerStore == nil {
			return &api.Response{Error: "worker store not initialized"}
		}
		var req struct {
			ActiveOnly bool `json:"active_only"`
		}
		_ = json.Unmarshal(data, &req)
		workers := env.WorkerStore.List(req.ActiveOnly)
		type row struct {
			TaskID          string              `json:"task_id"`
			WorkerID        string              `json:"worker_id"`
			Status          worker.WorkerStatus `json:"status"`
			Role            worker.Role         `json:"role"`
			TaskDescription string              `json:"task_description"`
			SpawnedAt       string              `json:"spawned_at"`
		}
		var out []row
		for _, w := range workers {
			tid := w.TaskID
			if tid == "" {
				tid = w.WorkerID
			}
			out = append(out, row{
				TaskID:          tid,
				WorkerID:        w.WorkerID,
				Status:          w.Status,
				Role:            w.Role,
				TaskDescription: w.TaskDescription,
				SpawnedAt:       w.SpawnedAt.UTC().Format(time.RFC3339),
			})
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: raw}
	}
}

func makeTasksStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.TaskID = strings.TrimSpace(req.TaskID)
		if req.TaskID == "" {
			return &api.Response{Error: "task_id is required"}
		}
		if env.WorkerStore == nil {
			return &api.Response{Error: "worker store not initialized"}
		}
		if w, ok := env.WorkerStore.Get(req.TaskID); ok {
			raw, _ := json.Marshal(w)
			return &api.Response{Success: true, Data: raw}
		}
		for _, w := range env.WorkerStore.List(false) {
			if w.TaskID == req.TaskID {
				raw, _ := json.Marshal(w)
				return &api.Response{Success: true, Data: raw}
			}
		}
		return &api.Response{Error: "task not found"}
	}
}

func makeTasksPauseStubHandler() api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "tasks.pause is not supported for worker-backed tasks in this build"}
	}
}

func makeTasksResumeStubHandler() api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "tasks.resume is not supported for worker-backed tasks in this build"}
	}
}

func makeTasksCancelStubHandler() api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "tasks.cancel is not supported for worker-backed tasks in this build"}
	}
}

func makeCourtDecisionsListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.Court == nil {
			return &api.Response{Error: "court engine not initialized"}
		}
		all := env.Court.ListSessions()
		type row struct {
			ID         string             `json:"id"`
			ProposalID string             `json:"proposal_id"`
			State      court.SessionState `json:"state"`
			Verdict    string             `json:"verdict,omitempty"`
			RiskScore  float64            `json:"risk_score"`
			StartedAt  string             `json:"started_at"`
			EndedAt    string             `json:"ended_at,omitempty"`
		}
		var rows []row
		for _, s := range all {
			if s == nil {
				continue
			}
			r := row{
				ID:         s.ID,
				ProposalID: s.ProposalID,
				State:      s.State,
				Verdict:    s.Verdict,
				RiskScore:  s.RiskScore,
				StartedAt:  s.StartedAt.UTC().Format(time.RFC3339),
			}
			if s.EndedAt != nil {
				r.EndedAt = s.EndedAt.UTC().Format(time.RFC3339)
			}
			rows = append(rows, r)
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: raw}
	}
}

func makeCourtDecisionsShowHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" {
			return &api.Response{Error: "id is required"}
		}
		if env.Court == nil {
			return &api.Response{Error: "court engine not initialized"}
		}
		s, ok := env.Court.GetSession(req.ID)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("decision session %q not found", req.ID)}
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: raw}
	}
}

func makeTeamListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		teams := env.TeamRegistry.list()
		raw, err := json.Marshal(teams)
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: raw}
	}
}

func makeTeamCreateHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		rec, err := env.TeamRegistry.create(req.Name)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		raw, _ := json.Marshal(rec)
		return &api.Response{Success: true, Data: raw}
	}
}

func makeTeamJoinHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			TeamID string `json:"team_id"`
			Member string `json:"member"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if err := env.TeamRegistry.join(req.TeamID, req.Member); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}

func makeTeamLeaveHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			TeamID string `json:"team_id"`
			Member string `json:"member"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if err := env.TeamRegistry.leave(req.TeamID, req.Member); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}

func makeTeamStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			TeamID string `json:"team_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		rec, ok := env.TeamRegistry.get(req.TeamID)
		if !ok {
			return &api.Response{Error: "team not found"}
		}
		raw, _ := json.Marshal(rec)
		return &api.Response{Success: true, Data: raw}
	}
}

func makeAutonomyShowHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		rec, ok := env.AutonomyRegistry.show(req.SessionID)
		if !ok {
			return &api.Response{Error: "no autonomy record for session"}
		}
		raw, _ := json.Marshal(rec)
		return &api.Response{Success: true, Data: raw}
	}
}

func makeAutonomyGrantHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
			Preset    string `json:"preset"`
			Duration  string `json:"duration"`
			Scope     string `json:"scope"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		var until time.Time
		if strings.TrimSpace(req.Duration) != "" {
			d, err := time.ParseDuration(req.Duration)
			if err != nil {
				return &api.Response{Error: "invalid duration: " + err.Error()}
			}
			until = time.Now().Add(d)
		}
		if err := env.AutonomyRegistry.grant(req.SessionID, req.Preset, req.Scope, until); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}

func makeAutonomyRevokeHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if err := env.AutonomyRegistry.revoke(req.SessionID); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}

func makeAutonomyResetHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if err := env.AutonomyRegistry.reset(req.SessionID); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}
