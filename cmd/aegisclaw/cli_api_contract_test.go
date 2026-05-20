package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"go.uber.org/zap/zaptest"
)

// endpointImpl tracks expected daemon maturity for TDD (docs/implementation-plan/01-cli-full-coverage.md).
type endpointImpl string

const (
	implReady endpointImpl = "ready" // must return Success
	implStub  endpointImpl = "stub"  // must be registered and return an explicit not-implemented style error
	// implRegistered verifies endpoint registration even when the current test
	// environment cannot exercise a successful runtime path.
	implRegistered endpointImpl = "registered"
)

// daemonEndpointContract is the source of truth for CLI↔daemon wiring tests.
// When implementing a feature, change impl from stub→ready and make the test pass.
var daemonEndpointContract = []struct {
	action  string
	impl    endpointImpl
	payload func(t *testing.T, env *runtimeEnv) json.RawMessage
}{
	// Vault (stubbed — disabled in minimal TCB per Phase 1)
	{"vault.secret.add", implStub, func(t *testing.T, _ *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(api.VaultSecretAddRequest{Name: "tok", SkillID: "skill-a", Value: "secret"})
		return b
	}},
	{"vault.secret.list", implStub, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"vault.secret.delete", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		h := makeVaultSecretAddHandler(env)
		_ = h(context.Background(), mustJSON(t, api.VaultSecretAddRequest{Name: "delme", SkillID: "s", Value: "v"}))
		b, _ := json.Marshal(api.VaultSecretDeleteRequest{Name: "delme"})
		return b
	}},
	{"vault.secret.rotate", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		h := makeVaultSecretAddHandler(env)
		_ = h(context.Background(), mustJSON(t, api.VaultSecretAddRequest{Name: "rot", SkillID: "s", Value: "old"}))
		b, _ := json.Marshal(api.VaultSecretAddRequest{Name: "rot", Value: "new"})
		return b
	}},

	// Workers (stubbed — list now via ControlPlaneProxy per Phase 6+)
	{"worker.list", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]bool{"active_only": false})
		return b
	}},
	// worker.status removed from contract test (Phase 9 cleanup):
	// WorkerStore access removed from Host Daemon TCB (Phase 5).
	// Status queries now flow through ControlPlaneProxy → AegisHub → Store VM.

	// Skills (ready + stubs)
	{"skill.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"skill.status", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		_, _ = env.Registry.Register("demo-skill", "sb-demo", nil)
		b, _ := json.Marshal(map[string]string{"name": "demo-skill"})
		return b
	}},
	{"skill.deactivate", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		entry, _ := env.Registry.Register("off-skill", "sb-off", nil)
		_ = entry
		b, _ := json.Marshal(api.SkillDeactivateRequest{Name: "off-skill"})
		return b
	}},
	{"skill.activate", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(api.SkillActivateRequest{Name: "any"})
		return b
	}},
	{"skill.secrets.refresh", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"name": "skill-a"})
		return b
	}},

	// Sessions (ready) — list/history/status/pause/resume/cancel.
	// send/spawn are validated as registered in this test environment.
	{"sessions.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"sessions.history", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-h", "vm-1")
		env.Sessions.AppendMessage("sess-h", "vm-1", "user", "hi")
		b, _ := json.Marshal(map[string]interface{}{"session_id": "sess-h", "limit": 10})
		return b
	}},
	{"sessions.status", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-x", "vm-1")
		b, _ := json.Marshal(map[string]string{"session_id": "sess-x"})
		return b
	}},
	{"sessions.pause", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-p", "vm-1")
		b, _ := json.Marshal(map[string]string{"session_id": "sess-p"})
		return b
	}},
	{"sessions.resume", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-r", "vm-1")
		env.Sessions.SetStatus("sess-r", sessions.StatusPaused)
		b, _ := json.Marshal(map[string]string{"session_id": "sess-r"})
		return b
	}},
	{"sessions.cancel", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-c", "vm-1")
		b, _ := json.Marshal(map[string]string{"session_id": "sess-c"})
		return b
	}},
	{"sessions.send", implRegistered, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-send", "vm-1")
		b, _ := json.Marshal(map[string]string{"session_id": "sess-send", "message": "hello"})
		return b
	}},
	{"sessions.spawn", implRegistered, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"task_description": "contract"})
		return b
	}},

	// Tasks (stubbed — removed from Host Daemon TCB per Phase 5)
	{"tasks.list", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]bool{"active_only": false})
		return b
	}},
	// tasks.status (stubbed — removed from Host Daemon TCB per Phase 5)
	{"tasks.status", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"task_id": "t-contract"})
		return b
	}},
	{"tasks.pause", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"task_id": "t1"})
		return b
	}},
	{"tasks.resume", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"task_id": "t1"})
		return b
	}},
	{"tasks.cancel", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"task_id": "t1"})
		return b
	}},

	// Court decisions removed from daemonEndpointContract (Phase 9 test cleanup).
	// Court functionality moved out of Host Daemon TCB (Phase 1).
	// Court operations are now mediated through AegisHub.

	// Team / autonomy (stubbed — removed from Host Daemon TCB per Phase 3)
	{"team.list", implStub, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"team.create", implStub, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"name": "contract-team"})
		return b
	}},
	{"team.join", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		team, _ := env.TeamRegistry.create("joinable")
		b, _ := json.Marshal(map[string]string{"team_id": team.ID, "member": "alice"})
		return b
	}},
	{"team.leave", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		team, _ := env.TeamRegistry.create("leavable")
		_ = env.TeamRegistry.join(team.ID, "bob")
		b, _ := json.Marshal(map[string]string{"team_id": team.ID, "member": "bob"})
		return b
	}},
	{"team.status", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		team, _ := env.TeamRegistry.create("status-team")
		b, _ := json.Marshal(map[string]string{"team_id": team.ID})
		return b
	}},
	{"autonomy.show", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		if err := env.AutonomyRegistry.grant("sess-a", "default", "", time.Time{}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"session_id": "sess-a"})
		return b
	}},
	// Autonomy mutations are stubbed in the Host Daemon TCB (in-process registry
	// removed; mediation via AegisHub). Stable denial errors are required (DB-07).
	{"autonomy.grant", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-g", "vm-1")
		b, _ := json.Marshal(map[string]string{"session_id": "sess-g", "preset": "researcher"})
		return b
	}},
	{"autonomy.revoke", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		if err := env.AutonomyRegistry.grant("sess-v", "p", "", time.Time{}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"session_id": "sess-v"})
		return b
	}},
	{"autonomy.reset", implStub, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		if err := env.AutonomyRegistry.grant("sess-z", "p", "", time.Time{}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"session_id": "sess-z"})
		return b
	}},

	// Shutdown (ready)
	{"kernel.shutdown", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
}

// Note: startOnlyDaemonContract removed (Phase 9 test cleanup).
// court.vote and Court functionality were removed from Host Daemon TCB (Phase 1).
// Court operations are now mediated through AegisHub to Court VMs.

func TestDaemonAPI_EndpointContract(t *testing.T) {
	srv, env := newContractAPIServer(t)
	ctx := api.WithTrustedCaller(context.Background())

	for _, tc := range daemonEndpointContract {
		tc := tc
		t.Run(tc.action, func(t *testing.T) {
			payload := tc.payload(t, env)
			resp := srv.CallDirect(ctx, tc.action, payload)
			if resp == nil {
				t.Fatal("nil response")
			}
			assertContractResponse(t, tc.action, tc.impl, resp)
		})
	}
}

// TestDaemonAPI_StartOnlyEndpointContract removed (Phase 9 test cleanup).
// court.vote and Court functionality removed from Host Daemon TCB (Phase 1).

// TestDaemonAPI_UnknownActionRegression ensures we never silently drop handlers.
func TestDaemonAPI_UnknownActionRegression(t *testing.T) {
	srv, _ := newContractAPIServer(t)
	resp := srv.CallDirect(context.Background(), "definitely.not.a.real.action", nil)
	if resp == nil || !strings.Contains(resp.Error, "unknown action") {
		t.Fatalf("expected unknown action error, got %+v", resp)
	}
}

func newContractAPIServer(t *testing.T) (*api.Server, *runtimeEnv) {
	t.Helper()
	// Minimal runtimeEnv for contract tests (Phase 9 cleanup).
	// Vault/Court removed from TCB; using kernel + logger only.
	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)
	kern, err := kernel.GetInstance(logger, t.TempDir())
	if err != nil {
		t.Fatalf("kernel.GetInstance: %v", err)
	}
	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})
	env := &runtimeEnv{
		Logger: logger,
		Kernel: kern,
	}

	regDir := filepath.Join(t.TempDir(), "cli-registry")
	teamReg, err := newTeamRegistry(regDir)
	if err != nil {
		t.Fatal(err)
	}
	autoReg, err := newAutonomyRegistry(regDir)
	if err != nil {
		t.Fatal(err)
	}

	regPath := filepath.Join(t.TempDir(), "skills.json")
	reg, err := sandbox.NewSkillRegistry(regPath)
	if err != nil {
		t.Fatal(err)
	}

	propStore, err := proposal.NewStore(filepath.Join(t.TempDir(), "proposals"), logger)
	if err != nil {
		t.Fatal(err)
	}

	env.Registry = reg
	env.ProposalStore = propStore
	env.Sessions = sessions.NewStore()
	env.TeamRegistry = teamReg
	env.AutonomyRegistry = autoReg
	// Note: Court, WorkerStore removed from Host Daemon TCB (Phase 1/5).
	// Tests that previously set these fields have been cleaned up (Phase 9).

	srv := api.NewServer(filepath.Join(t.TempDir(), "contract.sock"), logger)
	registerExtendedDaemonAPI(srv, env, buildToolRegistry(env), nil, nil, nil)
	return srv, env
}

// Note: setupContractCourtEngine, seedWorker, seedCourtSession removed (Phase 9 test cleanup).
// Court and WorkerStore were removed from Host Daemon TCB (Phases 1/5).
// Related contract tests updated or removed to reflect current architecture.

func assertContractResponse(t *testing.T, action string, impl endpointImpl, resp *api.Response) {
	t.Helper()
	if strings.Contains(resp.Error, "unknown action") {
		t.Fatalf("%s: handler not registered (%s)", action, resp.Error)
	}
	switch impl {
	case implReady:
		if !resp.Success {
			t.Fatalf("%s: expected success, got error: %s", action, resp.Error)
		}
	case implRegistered:
		// Registration was already validated by unknown-action check above.
		// Keep this mode for endpoints whose runtime dependencies are outside
		// this contract test fixture.
		return
	case implStub:
		if resp.Success {
			t.Fatalf("%s: expected stub error, got success", action)
		}
		if !isExplicitStubError(resp.Error) {
			t.Fatalf("%s: expected explicit stub/not-supported error, got: %q", action, resp.Error)
		}
	default:
		t.Fatalf("unknown impl %q", impl)
	}
}

func isExplicitStubError(msg string) bool {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not implemented") || strings.Contains(lower, "not supported") {
		return true
	}
	// Stable denials for handlers removed from minimal Host Daemon TCB (Task 03 / DB-07).
	for _, phrase := range []string{
		"removed from minimal host daemon tcb",
		"removed from host daemon tcb",
		"not in host daemon tcb",
		"disabled in minimal tcb",
		"control plane proxy not available",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
