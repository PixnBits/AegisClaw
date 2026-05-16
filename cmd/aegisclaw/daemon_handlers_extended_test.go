package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/sessions"
)

func testEnvWithSessions(t *testing.T) *runtimeEnv {
	t.Helper()
	env := testEnvWithVaultAndKernel(t)
	env.Sessions = sessions.NewStore()
	return env
}

func TestSessionsPauseResumeCancel(t *testing.T) {
	env := testEnvWithSessions(t)
	env.Sessions.Open("sess-1", "vm-1")

	statusH := makeSessionsStatusHandler(env)
	pauseH := makeSessionsPauseHandler(env)
	resumeH := makeSessionsResumeHandler(env)
	cancelH := makeSessionsCancelHandler(env)

	resp := callVaultHandler(t, statusH, map[string]string{"session_id": "sess-1"})
	if !resp.Success {
		t.Fatalf("status: %s", resp.Error)
	}

	resp = callVaultHandler(t, pauseH, map[string]string{"session_id": "sess-1"})
	if !resp.Success {
		t.Fatalf("pause: %s", resp.Error)
	}
	rec, _ := env.Sessions.Get("sess-1")
	if rec.Status != sessions.StatusPaused {
		t.Fatalf("want paused, got %s", rec.Status)
	}

	sendH := makeSessionsSendHandler(env, nil)
	resp = sendH(context.Background(), mustMarshalJSON(t, map[string]string{
		"session_id": "sess-1",
		"message":    "hi",
	}))
	if resp.Success {
		t.Fatal("expected send to fail while paused")
	}
	if resp.Error == "" {
		t.Fatal("expected error on send while paused")
	}

	resp = callVaultHandler(t, resumeH, map[string]string{"session_id": "sess-1"})
	if !resp.Success {
		t.Fatalf("resume: %s", resp.Error)
	}

	resp = callVaultHandler(t, cancelH, map[string]string{"session_id": "sess-1"})
	if !resp.Success {
		t.Fatalf("cancel: %s", resp.Error)
	}
	rec, _ = env.Sessions.Get("sess-1")
	if rec.Status != sessions.StatusClosed {
		t.Fatalf("want closed, got %s", rec.Status)
	}
}

func TestCourtDecisionsListEmpty(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	env.Court = nil
	h := makeCourtDecisionsListHandler(env)
	resp := h(context.Background(), nil)
	if resp.Success {
		t.Fatal("expected error without court engine")
	}
	if resp.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestTeamHandlersRoundTrip(t *testing.T) {
	dir := t.TempDir()
	reg, err := newTeamRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	env := &runtimeEnv{TeamRegistry: reg}

	createH := makeTeamCreateHandler(env)
	resp := callVaultHandler(t, createH, map[string]string{"name": "ops"})
	if !resp.Success {
		t.Fatalf("create: %s", resp.Error)
	}
	var team teamRecord
	if err := json.Unmarshal(resp.Data, &team); err != nil {
		t.Fatal(err)
	}

	joinH := makeTeamJoinHandler(env)
	resp = callVaultHandler(t, joinH, map[string]string{"team_id": team.ID, "member": "bob"})
	if !resp.Success {
		t.Fatalf("join: %s", resp.Error)
	}

	statusH := makeTeamStatusHandler(env)
	resp = callVaultHandler(t, statusH, map[string]string{"team_id": team.ID})
	if !resp.Success {
		t.Fatalf("status: %s", resp.Error)
	}
}

func TestTeamAutonomyHandlersRegisteredWithoutRegistry(t *testing.T) {
	env := &runtimeEnv{}
	srv := api.NewServer("", nil)
	registerExtendedDaemonAPI(srv, env, nil, nil, nil)

	for _, action := range []string{"team.list", "team.create", "team.join", "team.leave", "team.status"} {
		resp := srv.CallDirect(api.WithTrustedCaller(context.Background()), action, nil)
		if resp == nil || resp.Success {
			t.Fatalf("%s: expected initialization error, got %+v", action, resp)
		}
		if !strings.Contains(resp.Error, "team registry not initialized") {
			t.Fatalf("%s: unexpected error: %q", action, resp.Error)
		}
	}
	for _, action := range []string{"autonomy.show", "autonomy.grant", "autonomy.revoke", "autonomy.reset"} {
		resp := srv.CallDirect(api.WithTrustedCaller(context.Background()), action, nil)
		if resp == nil || resp.Success {
			t.Fatalf("%s: expected initialization error, got %+v", action, resp)
		}
		if !strings.Contains(resp.Error, "autonomy registry not initialized") {
			t.Fatalf("%s: unexpected error: %q", action, resp.Error)
		}
	}
}

func TestSessionsResumeRejectsClosedSession(t *testing.T) {
	env := testEnvWithSessions(t)
	env.Sessions.Open("sess-closed", "vm-1")
	env.Sessions.Close("sess-closed")
	resumeH := makeSessionsResumeHandler(env)
	resp := callVaultHandler(t, resumeH, map[string]string{"session_id": "sess-closed"})
	if resp.Success {
		t.Fatal("expected resume to fail for closed session")
	}
	if !strings.Contains(resp.Error, "not paused") {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

func TestSessionsSendRejectsClosedSession(t *testing.T) {
	env := testEnvWithSessions(t)
	env.Sessions.Open("sess-closed", "vm-1")
	env.Sessions.Close("sess-closed")
	sendH := makeSessionsSendHandler(env, nil)
	resp := sendH(context.Background(), mustMarshalJSON(t, map[string]string{
		"session_id": "sess-closed",
		"message":    "hello",
	}))
	if resp.Success {
		t.Fatal("expected send to fail for closed session")
	}
	if !strings.Contains(resp.Error, "session is closed") {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

func TestKernelShutdownRequiresCallerIdentity(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeKernelShutdownHandler(env, nil, nil, make(chan struct{}))

	resp := h(context.Background(), nil)
	if resp.Success {
		t.Fatal("expected shutdown to fail without caller identity")
	}
	if !strings.Contains(resp.Error, "authenticated local caller identity") {
		t.Fatalf("unexpected error: %q", resp.Error)
	}

	resp = h(api.WithTrustedCaller(context.Background()), nil)
	if !resp.Success {
		t.Fatalf("expected trusted caller to be allowed, got %q", resp.Error)
	}
}

func mustMarshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
