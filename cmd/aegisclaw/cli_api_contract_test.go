package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/google/uuid"
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
	// Vault (ready)
	{"vault.secret.add", implReady, func(t *testing.T, _ *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(api.VaultSecretAddRequest{Name: "tok", SkillID: "skill-a", Value: "secret"})
		return b
	}},
	{"vault.secret.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"vault.secret.delete", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		h := makeVaultSecretAddHandler(env)
		_ = h(context.Background(), mustJSON(t, api.VaultSecretAddRequest{Name: "delme", SkillID: "s", Value: "v"}))
		b, _ := json.Marshal(api.VaultSecretDeleteRequest{Name: "delme"})
		return b
	}},
	{"vault.secret.rotate", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		h := makeVaultSecretAddHandler(env)
		_ = h(context.Background(), mustJSON(t, api.VaultSecretAddRequest{Name: "rot", SkillID: "s", Value: "old"}))
		b, _ := json.Marshal(api.VaultSecretAddRequest{Name: "rot", Value: "new"})
		return b
	}},

	// Workers (ready)
	{"worker.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]bool{"active_only": false})
		return b
	}},
	{"worker.status", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		id := seedWorker(t, env)
		b, _ := json.Marshal(map[string]string{"worker_id": id})
		return b
	}},

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

	// Tasks (ready list/status; pause/resume/cancel stubbed)
	{"tasks.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]bool{"active_only": false})
		return b
	}},
	{"tasks.status", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		id := seedWorker(t, env)
		b, _ := json.Marshal(map[string]string{"task_id": id})
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

	// Court decisions (ready when engine present)
	{"court.decisions.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"court.decisions.show", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		sid := seedCourtSession(t, env)
		b, _ := json.Marshal(map[string]string{"id": sid})
		return b
	}},

	// Team / autonomy (ready)
	{"team.list", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
	{"team.create", implReady, func(*testing.T, *runtimeEnv) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"name": "contract-team"})
		return b
	}},
	{"team.join", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		team, _ := env.TeamRegistry.create("joinable")
		b, _ := json.Marshal(map[string]string{"team_id": team.ID, "member": "alice"})
		return b
	}},
	{"team.leave", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		team, _ := env.TeamRegistry.create("leavable")
		_ = env.TeamRegistry.join(team.ID, "bob")
		b, _ := json.Marshal(map[string]string{"team_id": team.ID, "member": "bob"})
		return b
	}},
	{"team.status", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		team, _ := env.TeamRegistry.create("status-team")
		b, _ := json.Marshal(map[string]string{"team_id": team.ID})
		return b
	}},
	{"autonomy.show", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		if err := env.AutonomyRegistry.grant("sess-a", "default", "", time.Time{}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"session_id": "sess-a"})
		return b
	}},
	{"autonomy.grant", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		env.Sessions.Open("sess-g", "vm-1")
		b, _ := json.Marshal(map[string]string{"session_id": "sess-g", "preset": "researcher"})
		return b
	}},
	{"autonomy.revoke", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		if err := env.AutonomyRegistry.grant("sess-v", "p", "", time.Time{}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"session_id": "sess-v"})
		return b
	}},
	{"autonomy.reset", implReady, func(t *testing.T, env *runtimeEnv) json.RawMessage {
		if err := env.AutonomyRegistry.grant("sess-z", "p", "", time.Time{}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]string{"session_id": "sess-z"})
		return b
	}},

	// Shutdown (ready)
	{"kernel.shutdown", implReady, func(*testing.T, *runtimeEnv) json.RawMessage { return nil }},
}

// startOnlyDaemonContract covers handlers registered in start.go outside registerExtendedDaemonAPI.
var startOnlyDaemonContract = []struct {
	action string
	impl   endpointImpl
}{
	{"court.vote", implStub},
}

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

func TestDaemonAPI_StartOnlyEndpointContract(t *testing.T) {
	srv, env := newContractAPIServer(t)
	ctx := api.WithTrustedCaller(context.Background())
	// court.vote is registered in start.go, not registerExtendedDaemonAPI.
	srv.Handle("court.vote", makeCourtVoteHandler(env, env.Court))

	for _, tc := range startOnlyDaemonContract {
		tc := tc
		t.Run(tc.action, func(t *testing.T) {
			resp := srv.CallDirect(ctx, tc.action, mustJSON(t, api.CourtVoteRequest{
				ProposalID: "p1",
				Voter:      "op",
				Approve:    true,
				Reason:     "test",
			}))
			assertContractResponse(t, tc.action, tc.impl, resp)
		})
	}
}

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
	env := testEnvWithVaultAndKernel(t)
	logger := env.Logger

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

	ws, err := worker.NewStore(filepath.Join(t.TempDir(), "workers"))
	if err != nil {
		t.Fatal(err)
	}

	propStore, err := proposal.NewStore(filepath.Join(t.TempDir(), "proposals"), logger)
	if err != nil {
		t.Fatal(err)
	}

	env.Registry = reg
	env.WorkerStore = ws
	env.ProposalStore = propStore
	env.Sessions = sessions.NewStore()
	env.TeamRegistry = teamReg
	env.AutonomyRegistry = autoReg
	env.Court = setupContractCourtEngine(t, env)

	srv := api.NewServer(filepath.Join(t.TempDir(), "contract.sock"), logger)
	registerExtendedDaemonAPI(srv, env, buildToolRegistry(env), nil, nil)
	return srv, env
}

func setupContractCourtEngine(t *testing.T, env *runtimeEnv) *court.Engine {
	t.Helper()
	personas := []*court.Persona{
		{Name: "CISO", Role: "security", SystemPrompt: "x", Models: []string{"m"}, Weight: 1.0},
	}
	reviewerFn := func(ctx context.Context, p *proposal.Proposal, persona *court.Persona) (*proposal.Review, error) {
		return &proposal.Review{
			ID:      uuid.New().String(),
			Persona: persona.Name,
			Model:   "m",
			Verdict: proposal.VerdictApprove,
		}, nil
	}
	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, env.ProposalStore, env.Kernel, personas, reviewerFn, env.Logger, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("court engine: %v", err)
	}
	return engine
}

func seedWorker(t *testing.T, env *runtimeEnv) string {
	t.Helper()
	id := uuid.New().String()
	if err := env.WorkerStore.Upsert(&worker.WorkerRecord{
		WorkerID:        id,
		Role:            worker.RoleResearcher,
		TaskDescription: "contract test",
		SpawnedBy:       "test",
		Status:          worker.StatusDone,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedCourtSession(t *testing.T, env *runtimeEnv) string {
	t.Helper()
	p, err := proposal.NewProposal("T", "D", proposal.CategoryNewSkill, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.ProposalStore.Create(p); err != nil {
		t.Fatal(err)
	}
	if err := p.Transition(proposal.StatusSubmitted, "ok", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.ProposalStore.Update(p); err != nil {
		t.Fatal(err)
	}
	sess, err := env.Court.VoteOnProposal(context.Background(), p.ID, "tester", true, "contract seed")
	if err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

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
	return strings.Contains(lower, "not implemented") ||
		strings.Contains(lower, "not supported")
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
