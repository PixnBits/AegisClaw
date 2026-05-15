package main

import (
	"context"
	"encoding/json"
	"testing"

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

func mustMarshalJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
