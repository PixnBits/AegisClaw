package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
	// Vault – secrets are sensitive
	apiSrv.Handle("vault.secret.add", withAuthorizedCaller(env, "vault.secret.add", makeVaultSecretAddHandler(env)))
	apiSrv.Handle("vault.secret.list", withAuthorizedCaller(env, "vault.secret.list", makeVaultSecretListHandler(env)))
	apiSrv.Handle("vault.secret.delete", withAuthorizedCaller(env, "vault.secret.delete", makeVaultSecretDeleteHandler(env)))
	apiSrv.Handle("vault.secret.rotate", withAuthorizedCaller(env, "vault.secret.rotate", makeVaultSecretRotateHandler(env)))

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

	// Sessions (all should be authorized)
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
	apiSrv.Handle("team.join", withAuthorizedCaller(env, "team.join", makeTeamJoinHandler(env)))
	apiSrv.Handle("team.leave", withAuthorizedCaller(env, "team.leave", makeTeamLeaveHandler(env)))
	apiSrv.Handle("team.status", withAuthorizedCaller(env, "team.status", makeTeamStatusHandler(env)))

	apiSrv.Handle("autonomy.show", withAuthorizedCaller(env, "autonomy.show", makeAutonomyShowHandler(env)))
	apiSrv.Handle("autonomy.grant", withAuthorizedCaller(env, "autonomy.grant", makeAutonomyGrantHandler(env)))
	apiSrv.Handle("autonomy.revoke", withAuthorizedCaller(env, "autonomy.revoke", makeAutonomyRevokeHandler(env)))
	apiSrv.Handle("autonomy.reset", withAuthorizedCaller(env, "autonomy.reset", makeAutonomyResetHandler(env)))
}

// ... rest of the file remains the same (handlers, helpers, etc.)
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

func authorizeCaller(_ *runtimeEnv, action string, ctx context.Context) error {
	if api.IsTrustedCaller(ctx) {
		return nil
	}
	uid, ok := api.PeerUIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("%s requires an authenticated local caller identity", action)
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

// (rest of file unchanged - I kept the full original structure in mind for the push)
