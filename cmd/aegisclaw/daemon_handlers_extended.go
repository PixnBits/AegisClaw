package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
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
	// Vault handlers stubbed — secrets handling has been removed from Host Daemon TCB.
	// All vault operations are now the responsibility of the Network Boundary VM only.
	apiSrv.Handle("vault.secret.add", makeVaultSecretAddHandler(env))
	apiSrv.Handle("vault.secret.list", makeVaultSecretListHandler(env))
	apiSrv.Handle("vault.secret.delete", makeVaultSecretDeleteHandler(env))
	apiSrv.Handle("vault.secret.rotate", makeVaultSecretRotateHandler(env))

	// Workers (read-only but still good to gate if needed)
	apiSrv.Handle("worker.list", makeWorkerListHandler(env))
	apiSrv.Handle("worker.status", makeWorkerStatusHandler(env))

	// Skills
	apiSrv.Handle("skill.list", makeSkillListHandler(env))
	apiSrv.Handle("skill.status", makeSkillStatusHandler(env))
	apiSrv.Handle("skill.deactivate", withAuthorizedCaller(env, "skill.deactivate", makeSkillDeactivateHandler(env)))
	apiSrv.Handle("skill.activate", withAuthorizedCaller(env, "skill.activate", makeSkillActivateHandler(env)))
	apiSrv.Handle("skill.secrets.refresh", withAuthorizedCaller(env, "skill.secrets.refresh", makeSkillSecretsRefreshHandler(env)))

	// Chat – message is public for portal/CLI, tool is gated
	apiSrv.Handle("chat.message", makeChatMessageHandler(env, toolRegistry))
	apiSrv.Handle("chat.slash", makeChatSlashHandler(env))
	apiSrv.Handle("chat.tool", withAuthorizedCaller(env, "chat.tool", makeChatToolExecHandler(env, toolRegistry)))
	apiSrv.Handle("chat.summarize", makeChatSummarizeHandler(env))

	// Kernel control (highly privileged) - now consistently wrapped
	apiSrv.Handle("kernel.shutdown", withAuthorizedCaller(env, "kernel.shutdown", makeKernelShutdownHandler(env, hub, apiSrv, daemonQuit)))
	apiSrv.Handle("kernel.restart", withAuthorizedCaller(env, "kernel.restart", makeKernelRestartHandler(env, hub, apiSrv, daemonQuit)))

	apiSrv.Handle("sessions.list", withAuthorizedCaller(env, "sessions.list", makeSessionsListHandler(env)))
	apiSrv.Handle("sessions.history", withAuthorizedCaller(env, "sessions.history", makeSessionsHistoryHandler(env)))
	apiSrv.Handle("sessions.send", withAuthorizedCaller(env, "sessions.send", makeSessionsSendHandler(env, toolRegistry)))
	apiSrv.Handle("sessions.spawn", withAuthorizedCaller(env, "sessions.spawn", makeSessionsSpawnHandler(env, toolRegistry)))
	apiSrv.Handle("sessions.status", withAuthorizedCaller(env, "sessions.status", makeSessionsStatusHandler(env)))
	apiSrv.Handle("sessions.pause", withAuthorizedCaller(env, "sessions.pause", makeSessionsPauseHandler(env)))
	apiSrv.Handle("sessions.resume", withAuthorizedCaller(env, "sessions.resume", makeSessionsResumeHandler(env)))
	apiSrv.Handle("sessions.cancel", withAuthorizedCaller(env, "sessions.cancel", makeSessionsCancelHandler(env)))

	// Tasks (stubs)
	apiSrv.Handle("tasks.list", makeTasksListHandler(env))
	apiSrv.Handle("tasks.status", makeTasksStatusHandler(env))
	apiSrv.Handle("tasks.pause", makeTasksPauseStubHandler())
	apiSrv.Handle("tasks.resume", makeTasksResumeStubHandler())
	apiSrv.Handle("tasks.cancel", makeTasksCancelStubHandler())

	// Court
	apiSrv.Handle("court.decisions.list", makeCourtDecisionsListHandler(env))
	apiSrv.Handle("court.decisions.show", makeCourtDecisionsShowHandler(env))

	// Team & Autonomy (new)
	apiSrv.Handle("team.list", withAuthorizedCaller(env, "team.list", makeTeamListHandler(env)))
	apiSrv.Handle("team.create", withAuthorizedCaller(env, "team.create", makeTeamCreateHandler(env)))
	apiSrv.Handle("team.new", withAuthorizedCaller(env, "team.new", makeTeamCreateHandler(env))) // alias for create per CLI spec
	apiSrv.Handle("team.join", withAuthorizedCaller(env, "team.join", makeTeamJoinHandler(env)))
	apiSrv.Handle("team.leave", withAuthorizedCaller(env, "team.leave", makeTeamLeaveHandler(env)))
	apiSrv.Handle("team.status", withAuthorizedCaller(env, "team.status", makeTeamStatusHandler(env)))

	apiSrv.Handle("autonomy.show", withAuthorizedCaller(env, "autonomy.show", makeAutonomyShowHandler(env)))
	apiSrv.Handle("autonomy.grant", withAuthorizedCaller(env, "autonomy.grant", makeAutonomyGrantHandler(env)))
	apiSrv.Handle("autonomy.revoke", withAuthorizedCaller(env, "autonomy.revoke", makeAutonomyRevokeHandler(env)))
	apiSrv.Handle("autonomy.reset", withAuthorizedCaller(env, "autonomy.reset", makeAutonomyResetHandler(env)))
}

// daemonOwnerUID returns the UID that should be treated as the daemon owner.
// It prefers explicit configuration, then falls back to SUDO_UID (common when
// the daemon was started via sudo), and finally the current effective UID.
//
// NOTE (Phase 5): Full peer-UID authorization via Unix socket credentials is
// primarily reliable on Linux and some BSDs. On Windows or other platforms
// without equivalent socket credential passing, PeerUIDFromContext may return
// false, causing authorizeCaller to reject non-trusted callers. The
// IsTrustedCaller path (used by the portal bridge) remains the escape hatch.
func daemonOwnerUID() int {
	if raw := strings.TrimSpace(os.Getenv("AEGIS_DAEMON_OWNER_UID")); raw != "" {
		if uid, err := strconv.Atoi(raw); err == nil && uid >= 0 {
			return uid
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SUDO_UID")); raw != "" {
		if uid, err := strconv.Atoi(raw); err == nil && uid >= 0 {
			return uid
		}
	}
	return os.Geteuid()
}

// isUnixLike reports whether the current OS supports reliable peer UID
// extraction from Unix domain sockets.
func isUnixLike() bool {
	goos := runtime.GOOS
	return goos == "linux" || goos == "darwin" || goos == "freebsd" || goos == "openbsd" || goos == "netbsd"
}

func authorizeCaller(_ *runtimeEnv, action string, ctx context.Context) error {
	if api.IsTrustedCaller(ctx) {
		return nil
	}
	uid, ok := api.PeerUIDFromContext(ctx)
	if !ok {
		if isUnixLike() {
			return fmt.Errorf("%s requires an authenticated local caller identity (peer UID not available)", action)
		}
		// On non-Unix platforms we still require either trusted-caller or explicit UID.
		return fmt.Errorf("%s requires an authenticated local caller identity (peer UID extraction not supported on %s)", action, runtime.GOOS)
	}
	ownerUID := daemonOwnerUID()
	if uid == 0 || uid == ownerUID {
		return nil
	}
	return fmt.Errorf("%s is restricted to daemon owner UID %d", action, ownerUID)
}

func withAuthorizedCaller(env *runtimeEnv, action string, h api.Handler) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if err := authorizeCaller(env, action, ctx); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return h(ctx, data)
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
			if err := env.Runtime.Stop(ctx, entry.SandboxID); err != nil {
				return &api.Response{Error: "stop sandbox: " + err.Error()}
			}
			if err := env.Runtime.Delete(ctx, entry.SandboxID); err != nil {
				return &api.Response{Error: "delete sandbox: " + err.Error()}
			}
		}
		if err := env.Registry.Deactivate(req.Name); err != nil {
			return &api.Response{Error: err.Error()}
		}
		payload, _ := json.Marshal(map[string]string{"skill_name": req.Name})
		action := kernel.NewAction(kernel.ActionSkillDeactivate, "api", payload)
		if _, err := env.Kernel.SignAndLog(action); err != nil {
			env.Logger.Error("failed to audit log skill deactivate", zap.Error(err))
		}
		respData, _ := json.Marshal(map[string]string{"message": "deactivated " + req.Name})
		return &api.Response{Success: true, Data: respData}
	}
}

func makeKernelShutdownHandler(env *runtimeEnv, hub *ipc.MessageHub, apiSrv *api.Server, daemonQuit chan struct{}) api.Handler {
	var mu sync.Mutex
	shutdownStarted := false
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		if err := authorizeCaller(env, "kernel.shutdown", ctx); err != nil {
			return &api.Response{Error: err.Error()}
		}
		mu.Lock()
		defer mu.Unlock()
		if shutdownStarted {
			data, _ := json.Marshal(map[string]string{"message": "shutdown already in progress"})
			return &api.Response{Success: true, Data: data}
		}
		payload, _ := json.Marshal(map[string]string{"reason": "kernel.shutdown"})
		action := kernel.NewAction(kernel.ActionKernelStop, "cli", payload)
		if _, err := env.Kernel.SignAndLog(action); err != nil {
			env.Logger.Error("failed to log kernel shutdown", zap.Error(err))
		}

		if err := shutdownRuntimeSandboxes(ctx, env); err != nil {
			return &api.Response{Error: err.Error()}
		}

		shutdownStarted = true
		scheduleDaemonExit(hub, apiSrv, daemonQuit)
		data, _ := json.Marshal(map[string]string{"message": "shutdown initiated"})
		return &api.Response{Success: true, Data: data}
	}
}

func makeKernelRestartHandler(env *runtimeEnv, hub *ipc.MessageHub, apiSrv *api.Server, daemonQuit chan struct{}) api.Handler {
	var mu sync.Mutex
	restartStarted := false
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		if err := authorizeCaller(env, "kernel.restart", ctx); err != nil {
			return &api.Response{Error: err.Error()}
		}
		mu.Lock()
		defer mu.Unlock()
		if restartStarted {
			data, _ := json.Marshal(map[string]string{"message": "restart already in progress"})
			return &api.Response{Success: true, Data: data}
		}
		if err := shutdownRuntimeSandboxes(ctx, env); err != nil {
			return &api.Response{Error: err.Error()}
		}
		exePath, err := os.Executable()
		if err != nil {
			return &api.Response{Error: fmt.Errorf("resolve executable path: %w", err).Error()}
		}
		restartProc := exec.Command(exePath, "start", "--foreground", "--allow-existing-daemon")
		restartProc.Stdout = os.Stdout
		restartProc.Stderr = os.Stderr
		restartProc.Env = restartChildEnv()
		if err := restartProc.Start(); err != nil {
			return &api.Response{Error: fmt.Errorf("start replacement daemon: %w", err).Error()}
		}
		payload, _ := json.Marshal(map[string]string{"reason": "kernel.restart"})
		action := kernel.NewAction(kernel.ActionKernelStop, "cli", payload)
		if _, err := env.Kernel.SignAndLog(action); err != nil {
			env.Logger.Error("failed to log kernel restart", zap.Error(err))
		}
		restartStarted = true
		scheduleDaemonExit(hub, apiSrv, daemonQuit)
		data, _ := json.Marshal(map[string]interface{}{
			"message": "restart initiated",
			"pid":     restartProc.Process.Pid,
		})
		return &api.Response{Success: true, Data: data}
	}
}

func shutdownRuntimeSandboxes(ctx context.Context, env *runtimeEnv) error {
	if env == nil || env.Runtime == nil {
		return nil
	}
	sandboxes, err := env.Runtime.List(ctx)
	if err != nil {
		return fmt.Errorf("list sandboxes: %w", err)
	}
	for _, sb := range sandboxes {
		id := strings.TrimSpace(sb.Spec.ID)
		if id == "" {
			continue
		}
		if sb.State == sandbox.StateRunning {
			if err := env.Runtime.Stop(ctx, id); err != nil {
				return fmt.Errorf("stop sandbox %s: %w", id, err)
			}
		}
		if err := env.Runtime.Delete(ctx, id); err != nil {
			return fmt.Errorf("delete sandbox %s: %w", id, err)
		}
	}
	return nil
}

func scheduleDaemonExit(hub *ipc.MessageHub, apiSrv *api.Server, daemonQuit chan struct{}) {
	go func() {
		time.Sleep(50 * time.Millisecond)
		if hub != nil {
			hub.Stop()
		}
		if apiSrv != nil {
			apiSrv.Stop()
		}
		if daemonQuit != nil {
			func() {
				defer func() { _ = recover() }()
				close(daemonQuit)
			}()
		}
	}()
}

func restartChildEnv() []string {
	const (
		pathKey       = "PATH="
		homeKey       = "HOME="
		userKey       = "USER="
		shellKey      = "SHELL="
		tmpKey        = "TMPDIR="
		langKey       = "LANG="
		xdgRuntimeKey = "XDG_RUNTIME_DIR="
		sudoUIDKey    = "SUDO_UID="
		sudoGIDKey    = "SUDO_GID="
		sudoUserKey   = "SUDO_USER="
	)
	allowPrefixes := []string{
		"AEGIS_",
		"LC_",
	}
	allowExact := []string{
		pathKey, homeKey, userKey, shellKey, tmpKey, langKey,
		xdgRuntimeKey, sudoUIDKey, sudoGIDKey, sudoUserKey,
	}
	env := make([]string, 0, 16)
outer:
	for _, kv := range os.Environ() {
		for _, key := range allowExact {
			if strings.HasPrefix(kv, key) {
				env = append(env, kv)
				continue outer
			}
		}
		for _, prefix := range allowPrefixes {
			if strings.HasPrefix(kv, prefix) {
				env = append(env, kv)
				continue outer
			}
		}
	}
	return env
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
	return func(_ context.Context, data json.RawMessage) *api.Response {
		// Court engine removed from daemon TCB per Phase 1; stubbed.
		_ = env
		return &api.Response{Error: "court engine not in Host Daemon TCB (Phase 1)"}
	}
}

func makeCourtDecisionsShowHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		// Court removed from TCB.
		_ = env
		return &api.Response{Error: "court not in Host Daemon TCB"}
	}
}
func makeTeamListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.TeamRegistry == nil {
			return &api.Response{Error: "team registry not initialized"}
		}
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
		if env.TeamRegistry == nil {
			return &api.Response{Error: "team registry not initialized"}
		}
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
		if env.TeamRegistry == nil {
			return &api.Response{Error: "team registry not initialized"}
		}
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
		if env.TeamRegistry == nil {
			return &api.Response{Error: "team registry not initialized"}
		}
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
		if env.TeamRegistry == nil {
			return &api.Response{Error: "team registry not initialized"}
		}
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
		if env.AutonomyRegistry == nil {
			return &api.Response{Error: "autonomy registry not initialized"}
		}
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		rec, ok, err := env.AutonomyRegistry.show(req.SessionID)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		if !ok {
			return &api.Response{Error: "no autonomy record for session"}
		}
		// Enforce expiry at read time (comprehensive fix)
		if rec.ExpiresAt != "" {
			expiry, parseErr := time.Parse(time.RFC3339, rec.ExpiresAt)
			if parseErr == nil && time.Now().UTC().After(expiry) {
				return &api.Response{Error: "autonomy grant has expired"}
			}
		}
		raw, _ := json.Marshal(rec)
		return &api.Response{Success: true, Data: raw}
	}
}

func makeAutonomyGrantHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.AutonomyRegistry == nil {
			return &api.Response{Error: "autonomy registry not initialized"}
		}
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
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		if _, ok := env.Sessions.Get(req.SessionID); !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		var until time.Time
		if strings.TrimSpace(req.Duration) != "" {
			d, err := time.ParseDuration(req.Duration)
			if err != nil {
				return &api.Response{Error: "invalid duration: " + err.Error()}
			}
			if d <= 0 {
				return &api.Response{Error: "duration must be greater than 0"}
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
		if env.AutonomyRegistry == nil {
			return &api.Response{Error: "autonomy registry not initialized"}
		}
		var req struct {
			SessionID string `json:"session_id"`
			Scope     string `json:"scope"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if err := env.AutonomyRegistry.revoke(req.SessionID, req.Scope); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}

func makeAutonomyResetHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.AutonomyRegistry == nil {
			return &api.Response{Error: "autonomy registry not initialized"}
		}
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if err := env.AutonomyRegistry.reset(req.SessionID); err != nil {
			return &api.Response{Error: err.Error()}
		}
		return &api.Response{Success: true}
	}
}
