package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"AegisClaw/internal/channelfacilitator"
	"AegisClaw/internal/chatstore"
)

func testStoreWorld(t *testing.T) *storeWorld {
	t.Helper()
	audit := []interface{}{}
	return &storeWorld{
		proposals:    map[string]interface{}{},
		skills:       map[string]interface{}{},
		memories:     map[string]interface{}{},
		prs:          map[string]interface{}{},
		teams:        map[string]interface{}{},
		channels:     map[string]interface{}{"keep": map[string]interface{}{"id": "keep"}},
		auditLog:     &audit,
		chatSessions: chatstore.New(filepath.Join(t.TempDir(), "chat-sessions.json")),
	}
}

func storeSnapshot(w *storeWorld) string {
	b, err := json.Marshal(map[string]interface{}{
		"proposals":   w.proposals,
		"skills":      w.skills,
		"memories":    w.memories,
		"prs":         w.prs,
		"teams":       w.teams,
		"channels":    w.channels,
		"audit":       *w.auditLog,
		"revocations": revocations,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func dispatchPayload(w *storeWorld, command string, payload interface{}) Message {
	resp := Message{Timestamp: "2026-10-06T00:00:00Z"}
	dispatchStoreCommand(Message{Source: "test", Command: command, Payload: payload}, &resp, w)
	return resp
}

func assertErrorReply(t *testing.T, resp Message, substr string) {
	t.Helper()
	if resp.Command != "error" {
		t.Fatalf("command %q payload %#v, want error", resp.Command, resp.Payload)
	}
	got := fmt.Sprint(resp.Payload)
	if substr != "" && !strings.Contains(got, substr) {
		t.Fatalf("payload %q, want substring %q", got, substr)
	}
}

func assertUnchanged(t *testing.T, w *storeWorld, before string, beforeFiles []string) {
	t.Helper()
	if got := storeSnapshot(w); got != before {
		t.Fatalf("store state changed\nbefore %s\nafter  %s", before, got)
	}
	after, err := filepath.Glob("*.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(after, ",") != strings.Join(beforeFiles, ",") {
		t.Fatalf("json files = %v, want %v", after, beforeFiles)
	}
}

func TestHandleChannelCreateMalformedPayload(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	ts := "2026-10-06T00:00:00Z"
	cases := []struct {
		name    string
		payload interface{}
		want    string
	}{
		{"members string", map[string]interface{}{"id": "main", "members": "pm"}, "members must be an array"},
		{"members number", map[string]interface{}{"id": "main", "members": float64(1)}, "members must be an array"},
		{"members int", map[string]interface{}{"id": "main", "members": 1}, "members must be an array"},
		{"members object", map[string]interface{}{"id": "main", "members": map[string]interface{}{"role": "pm"}}, "members must be an array"},
		{"payload string", "nope", "payload must be an object"},
		{"payload number", float64(1), "payload must be an object"},
		{"payload null", nil, "payload must be an object"},
		{"id number", map[string]interface{}{"id": float64(1)}, "id must be a string"},
		{"id missing", map[string]interface{}{}, "invalid channel id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channels := map[string]interface{}{}
			resp := handleChannelCreate(tc.payload, channels, ts)
			assertErrorReply(t, resp, tc.want)
			if len(channels) != 0 {
				t.Fatalf("stored %#v", channels)
			}
			if matches, _ := filepath.Glob("*.json"); len(matches) != 0 {
				t.Fatalf("wrote %v", matches)
			}
		})
	}
}

func TestHandleChannelCreateNullMembersMatchMissing(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	ts := "2026-10-06T00:00:00Z"
	missingCh := map[string]interface{}{}
	nullCh := map[string]interface{}{}
	emptyCh := map[string]interface{}{}
	missing := handleChannelCreate(map[string]interface{}{"id": "main"}, missingCh, ts)
	nulls := handleChannelCreate(map[string]interface{}{"id": "main", "members": nil}, nullCh, ts)
	empty := handleChannelCreate(map[string]interface{}{"id": "main", "members": []interface{}{}}, emptyCh, ts)
	for _, resp := range []Message{missing, nulls, empty} {
		if resp.Command != "channel.created" {
			t.Fatalf("command %q payload %#v", resp.Command, resp.Payload)
		}
	}
	mb, _ := json.Marshal(missingCh["main"])
	nb, _ := json.Marshal(nullCh["main"])
	eb, _ := json.Marshal(emptyCh["main"])
	if string(mb) != string(nb) || string(mb) != string(eb) {
		t.Fatalf("missing %s\nnull %s\nempty %s", mb, nb, eb)
	}
	members := missingCh["main"].(map[string]interface{})["members"].([]interface{})
	if len(members) != 1 || members[0].(map[string]interface{})["role"] != "project-manager" {
		t.Fatalf("default member %#v", members)
	}

	kept := map[string]interface{}{}
	resp := handleChannelCreate(map[string]interface{}{
		"id":      "main",
		"members": []interface{}{map[string]interface{}{"role": "coder"}},
	}, kept, ts)
	if resp.Command != "channel.created" {
		t.Fatalf("explicit members: %q %#v", resp.Command, resp.Payload)
	}
	got := kept["main"].(map[string]interface{})["members"].([]interface{})
	if len(got) != 1 || got[0].(map[string]interface{})["role"] != "coder" {
		t.Fatalf("explicit members stored %#v", got)
	}
}

// assertLLMUsageMalformedNoPanic sends the table's non-object payloads, plus
// bad, through dispatchStoreCommand. record is a one-way push: skip the reply
// and store nothing unless the source is network-boundary and the payload is
// an object. summary and recent reply with their own command. None may panic.
func assertLLMUsageMalformedNoPanic(t *testing.T, command string, bad map[string]interface{}) {
	t.Helper()
	type srcPayload struct {
		source  string
		payload interface{}
	}
	var cases []srcPayload
	for _, p := range []interface{}{"nope", float64(1), nil, bad} {
		cases = append(cases, srcPayload{"test", p})
	}
	if command == "llm.usage.record" {
		// The handler checks source before the payload assert. These reach it.
		for _, p := range []interface{}{"nope", float64(1), nil} {
			cases = append(cases, srcPayload{"network-boundary", p})
		}
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.source, "/", tc.payload), func(t *testing.T) {
			useLLMUsage(t)
			w := testStoreWorld(t)
			before := storeSnapshot(w)
			files, _ := filepath.Glob("*.json")
			resp := Message{Timestamp: "2026-10-06T00:00:00Z"}
			skip := dispatchStoreCommand(Message{
				Source: tc.source, Command: command, Payload: tc.payload,
			}, &resp, w)
			if command == "llm.usage.record" {
				if !skip {
					t.Fatal("skipReply = false, want true")
				}
				if _, isMap := tc.payload.(map[string]interface{}); tc.source != "network-boundary" || !isMap {
					if n := len(llmUsageSnapshot()); n != 0 {
						t.Fatalf("stored %d records", n)
					}
				}
			} else if skip || resp.Command != command {
				t.Fatalf("skipReply=%v command %q, want false and %q", skip, resp.Command, command)
			}
			assertUnchanged(t, w, before, files)
		})
	}
}

func TestDispatchMalformedPayloads(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	nonObjects := []struct {
		name    string
		payload interface{}
	}{
		{"string", "nope"},
		{"number", float64(1)},
		{"null", nil},
	}
	commands := []struct {
		command string
		bad     map[string]interface{}
		want    string
	}{
		{"timer.schedule", map[string]interface{}{"id": 1}, "id must be a string"},
		{"timer.cancel", map[string]interface{}{"id": 1}, "id must be a string"},
		{"autonomy.grant", map[string]interface{}{"session_id": 1}, "session_id must be a string"},
		{"grant.get", map[string]interface{}{"session_id": 1}, "session_id must be a string"},
		{"proposal.get", map[string]interface{}{"id": 1}, "id must be a string"},
		{"proposal.update", map[string]interface{}{"id": 1}, "id must be a string"},
		{"court.review_complete", map[string]interface{}{"proposal_id": "p", "votes": "no"}, "votes must be an object"},
		{"court.get_reviews", map[string]interface{}{"id": 1}, "id must be a string"},
		{"court.record_enforcement", map[string]interface{}{"agent_id": 1}, "agent_id must be a string"},
		{"pr.create", map[string]interface{}{"id": 1}, "id must be a string"},
		{"pr.update", map[string]interface{}{"id": 1}, "id must be a string"},
		{"pr.get", map[string]interface{}{"id": 1}, "id must be a string"},
		{"secrets.push", map[string]interface{}{"secrets": "nope"}, "secrets must be an object"},
		{"team.create", map[string]interface{}{"id": 1}, "id must be a string"},
		{"team.get", map[string]interface{}{"id": 1}, "id must be a string"},
		{"team.message", map[string]interface{}{"team_id": 1}, "team_id must be a string"},
		{"channel.create", map[string]interface{}{"id": "main", "members": "pm"}, "members must be an array"},
		{"channel.get", map[string]interface{}{"id": 1}, "id must be a string"},
		{"channel.join", map[string]interface{}{"channel_id": 1}, "channel_id must be a string"},
		{"channel.post", map[string]interface{}{"channel_id": 1}, "channel_id must be a string"},
		{"channel.archive", map[string]interface{}{"id": 1}, "id must be a string"},
		{"channel.add_member", map[string]interface{}{"id": 1}, "id must be a string"},
		{"channel.remove_member", map[string]interface{}{"role": 1}, "role must be a string"},
		{channelfacilitator.CmdMemberTurnUpdate, map[string]interface{}{"role": 1}, "role must be a string"},
		{channelfacilitator.CmdTurnState, map[string]interface{}{"id": 1}, "id must be a string"},
		{channelfacilitator.CmdGetMessages, map[string]interface{}{"filter": "x"}, "filter must be an object"},
		{channelfacilitator.CmdGetRelevantSince, map[string]interface{}{"anchor_seqs": "x"}, "anchor_seqs must be an array"},
		{"sessions.create", map[string]interface{}{"title": 1}, "title must be a string"},
		{"sessions.history", map[string]interface{}{"session_id": 1}, "session_id must be a string"},
		{"sessions.get", map[string]interface{}{"id": 1}, "id must be a string"},
		{"sessions.save", map[string]interface{}{"id": "sess", "messages": "nope"}, "messages must be an array"},
		{"skill.register", map[string]interface{}{"id": 1}, "id must be a string"},
		{"skill.get", map[string]interface{}{"id": 1}, "id must be a string"},
		{"build.complete", map[string]interface{}{"proposal_id": 1}, "proposal_id must be a string"},
		{"build.failed", map[string]interface{}{"proposal_id": 1}, "proposal_id must be a string"},
		{"memory.store", map[string]interface{}{"content": 1}, "content must be a string"},
		// Not mustPayload commands. A bad payload must not panic: record skips
		// the reply, summary and recent still reply.
		{"llm.usage.record", map[string]interface{}{"tokens_prompt": "nope"}, "no panic"},
		{"llm.usage.summary", map[string]interface{}{"agent_id": 1}, "no panic"},
		{"llm.usage.recent", map[string]interface{}{"limit": "x"}, "no panic"},
	}
	for _, cmd := range commands {
		t.Run(cmd.command, func(t *testing.T) {
			if cmd.want == "no panic" {
				assertLLMUsageMalformedNoPanic(t, cmd.command, cmd.bad)
				return
			}
			for _, raw := range nonObjects {
				t.Run(raw.name, func(t *testing.T) {
					w := testStoreWorld(t)
					before := storeSnapshot(w)
					files, _ := filepath.Glob("*.json")
					resp := dispatchPayload(w, cmd.command, raw.payload)
					want := "payload must be an object"
					if cmd.command == "proposal.get" {
						want = "ERR_BAD_PAYLOAD"
					}
					assertErrorReply(t, resp, want)
					assertUnchanged(t, w, before, files)
				})
			}
			t.Run("wrong field", func(t *testing.T) {
				w := testStoreWorld(t)
				before := storeSnapshot(w)
				files, _ := filepath.Glob("*.json")
				resp := dispatchPayload(w, cmd.command, cmd.bad)
				assertErrorReply(t, resp, cmd.want)
				assertUnchanged(t, w, before, files)
			})
		})
	}
}

func TestChannelJoinRejectsCorruptStoredMembers(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	w.channels["main"] = map[string]interface{}{"members": "corrupt"}
	resp := dispatchPayload(w, "channel.join", map[string]interface{}{"channel_id": "main", "role": "coder"})
	assertErrorReply(t, resp, "invalid stored channel: members must be an array")
	if w.channels["main"].(map[string]interface{})["members"] != "corrupt" {
		t.Fatalf("members rewritten: %#v", w.channels["main"])
	}
}

func TestValidChannelPostStillAppends(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w.priv = priv
	w.encoder = json.NewEncoder(&buf)
	w.channels["main"] = map[string]interface{}{
		"id":       "main",
		"members":  []interface{}{},
		"messages": []interface{}{},
		"next_seq": 1,
	}
	resp := dispatchPayload(w, "channel.post", map[string]interface{}{
		"channel_id": "main",
		"from":       "pm",
		"content":    "hello",
	})
	if resp.Command != "channel.posted" || resp.Payload != "ok" {
		t.Fatalf("post: %q %#v", resp.Command, resp.Payload)
	}
	msgs := w.channels["main"].(map[string]interface{})["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("messages %#v", msgs)
	}
	if buf.Len() == 0 {
		t.Fatal("channel.updated was not emitted")
	}
}
