package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// TestExtendedDaemonAPI_TCBStableDenialsFullTable is the DB-07 regression suite:
// every non-core handler registered in registerExtendedDaemonAPI that must not
// silently succeed in the minimal Host Daemon TCB (nil ControlPlaneProxy) is
// exercised here. When adding apiSrv.Handle(...) entries, extend this table.
func TestExtendedDaemonAPI_TCBStableDenialsFullTable(t *testing.T) {
	srv, env := newContractAPIServer(t)
	ctx := api.WithTrustedCaller(context.Background())

	team, err := env.TeamRegistry.create("tcb-deny-table")
	if err != nil {
		t.Fatal(err)
	}
	_ = env.TeamRegistry.join(team.ID, "member")

	env.Sessions.Open("tcb-send", "vm-1")

	cases := []struct {
		action  string
		payload json.RawMessage
	}{
		{"vault.secret.add", mustJSON(t, api.VaultSecretAddRequest{Name: "n", SkillID: "s", Value: "v"})},
		{"vault.secret.list", json.RawMessage(`{}`)},
		{"vault.secret.delete", mustJSON(t, api.VaultSecretDeleteRequest{Name: "x"})},
		{"vault.secret.rotate", mustJSON(t, api.VaultSecretAddRequest{Name: "r", Value: "v"})},
		{"worker.list", mustJSON(t, map[string]bool{"active_only": false})},
		{"worker.status", mustJSON(t, map[string]string{"worker_id": "w-1"})},
		{"proposal.list", json.RawMessage(`{}`)},
		{"proposal.status", mustJSON(t, map[string]string{"proposal_id": "p1"})},
		{"chat.message", mustJSON(t, api.ChatMessageRequest{Input: "hi", SessionID: "tcb-send"})},
		{"chat.slash", mustJSON(t, api.ChatSlashRequest{Command: "/help"})},
		{"chat.tool", mustJSON(t, api.ChatToolExecRequest{Name: "t", Args: "{}"})},
		{"chat.summarize", mustJSON(t, api.ChatSummarizeRequest{ToolName: "t", ToolResult: "r"})},
		{"skill.activate", mustJSON(t, api.SkillActivateRequest{Name: "any"})},
		{"skill.secrets.refresh", mustJSON(t, map[string]string{"name": "skill-a"})},
		{"tasks.list", mustJSON(t, map[string]bool{"active_only": false})},
		{"tasks.status", mustJSON(t, map[string]string{"task_id": "t1"})},
		{"tasks.pause", mustJSON(t, map[string]string{"task_id": "t1"})},
		{"tasks.resume", mustJSON(t, map[string]string{"task_id": "t1"})},
		{"tasks.cancel", mustJSON(t, map[string]string{"task_id": "t1"})},
		{"court.decisions.list", json.RawMessage(`{}`)},
		{"court.decisions.show", mustJSON(t, map[string]string{"decision_id": "d1"})},
		{"team.list", nil},
		{"team.create", mustJSON(t, map[string]string{"name": "n"})},
		{"team.new", mustJSON(t, map[string]string{"name": "n2"})},
		{"team.join", mustJSON(t, map[string]string{"team_id": team.ID, "member": "alice"})},
		{"team.leave", mustJSON(t, map[string]string{"team_id": team.ID, "member": "member"})},
		{"team.status", mustJSON(t, map[string]string{"team_id": team.ID})},
		{"autonomy.show", mustJSON(t, map[string]string{"session_id": "sess-a"})},
		{"autonomy.grant", mustJSON(t, map[string]string{"session_id": "sess-g", "preset": "p"})},
		{"autonomy.revoke", mustJSON(t, map[string]string{"session_id": "sess-v"})},
		{"autonomy.reset", mustJSON(t, map[string]string{"session_id": "sess-z"})},
		{"sessions.send", mustJSON(t, map[string]string{"session_id": "tcb-send", "message": "m"})},
		{"sessions.spawn", mustJSON(t, map[string]string{"task_description": "x"})},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.action, func(t *testing.T) {
			resp := srv.CallDirect(ctx, tc.action, tc.payload)
			if resp == nil {
				t.Fatal("nil response")
			}
			if strings.Contains(resp.Error, "unknown action") {
				t.Fatalf("handler not registered: %s", resp.Error)
			}
			if resp.Success {
				t.Fatalf("expected failure for minimal-TCB denial path")
			}
			if !isExplicitStubError(resp.Error) {
				t.Fatalf("expected explicit stub/TCB denial, got: %q", resp.Error)
			}
		})
	}
}
