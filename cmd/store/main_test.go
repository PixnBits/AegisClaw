package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadSaveFromFile(t *testing.T) {
	filename := "test_store.json"
	defer os.Remove(filename)

	data := map[string]interface{}{"key": "value"}
	saveToFile(filename, data)

	loaded := loadFromFile(filename)
	if loaded["key"] != "value" {
		t.Errorf("Expected value, got %v", loaded["key"])
	}
}

func TestSignMessage(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	msg := &Message{
		Source:    "test",
		Command:   "test",
		Payload:   "data",
		Timestamp: "2026-05-10T00:00:00Z",
	}
	signMessage(msg, priv)
	if msg.Signature == "" {
		t.Error("Signature not set")
	}

	data, _ := json.Marshal(Message{Source: "test", Command: "test", Payload: "data", Timestamp: "2026-05-10T00:00:00Z"})
	sigBytes, _ := base64.StdEncoding.DecodeString(msg.Signature)
	if !ed25519.Verify(pub, data, sigBytes) {
		t.Error("Signature verification failed")
	}
}

func TestStoreCommands(t *testing.T) {
	withTempDir(t, func() {
		// Drive real handler path (canCreate + performProposalCreate) instead of manual map poke.
		proposals := make(map[string]interface{})
		payload := map[string]interface{}{
			"id":          "test_proposal",
			"description": "test",
		}
		if err := canCreateProposal("client", payload); err != nil {
			t.Fatalf("privileged source should allow via real gate: %v", err)
		}
		id := payload["id"].(string)
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		performProposalCreate(id, payload, proposals, nil, priv, time.Now().Format(time.RFC3339))

		if proposals["test_proposal"] == nil {
			t.Error("Proposal not created via real performProposalCreate")
		}
		onDisk := loadFromFile("proposals.json")
		if onDisk["test_proposal"] == nil {
			t.Error("proposals.json not written by real handler in TestStoreCommands")
		}
	})
}

func TestComputeMerkleRoot(t *testing.T) {
	log := []interface{}{"entry1", "entry2"}
	root := computeMerkleRoot(log)
	if root == "" {
		t.Error("Root should not be empty")
	}
	// Test empty
	emptyRoot := computeMerkleRoot([]interface{}{})
	if emptyRoot != "" {
		t.Error("Empty root should be empty string")
	}
}

// fakeWriter captures json.Encoder writes for asserting scribe send side-effect in tests.
type fakeWriter struct{ msgs *[]Message }

func (f *fakeWriter) Write(p []byte) (int, error) {
	var m Message
	json.Unmarshal(p, &m)
	if f.msgs != nil {
		*f.msgs = append(*f.msgs, m)
	}
	return len(p), nil
}

// TestCanCreateProposalHappyAndDenied exercises the real permission check used by
// proposal.create handler (happy for privileged + grant, denied for low-priv).
// One happy, one ERR_PERMISSION_DENIED case as required.
func TestCanCreateProposalHappyAndDenied(t *testing.T) {
	// privileged sources succeed without grants
	for _, src := range []string{"daemon-internal", "aegis-cli-internal-123", "client", "store"} {
		if err := canCreateProposal(src, map[string]interface{}{"id": "p1"}); err != nil {
			t.Errorf("privileged source %s should succeed, got: %v", src, err)
		}
	}

	// low-priv without grant -> ERR_PERMISSION_DENIED and message
	err := canCreateProposal("agent-lowpriv-xyz", map[string]interface{}{"id": "p2"})
	if err == nil || !strings.Contains(err.Error(), "ERR_PERMISSION_DENIED") {
		t.Errorf("low priv agent source must get ERR_PERMISSION_DENIED, got: %v", err)
	}

	err = canCreateProposal("builder-foo", nil)
	if err == nil || !strings.Contains(err.Error(), "ERR_PERMISSION_DENIED") {
		t.Errorf("builder low priv must get ERR_PERMISSION_DENIED, got: %v", err)
	}

	// with a grant present for the source, allow (use in-mem by writing temp grants.json)
	grantFile := "grants.json"
	orig, _ := os.ReadFile(grantFile)
	defer func() {
		if orig != nil {
			os.WriteFile(grantFile, orig, 0600)
		} else {
			os.Remove(grantFile)
		}
	}()
	grantData := map[string]interface{}{
		"agent-sess-allow": map[string]interface{}{
			"scopes": []interface{}{"proposal.create"},
		},
	}
	b, _ := json.Marshal(grantData)
	os.WriteFile(grantFile, b, 0600)
	if err := canCreateProposal("agent-sess-allow", nil); err != nil {
		t.Errorf("source with grant for proposal.create must succeed: %v", err)
	}
	os.Remove(grantFile) // clean for other tests

	// Additional case for the fixed logic: unrelated grant scopes must NOT authorize proposal.create
	grantUnrelated := map[string]interface{}{
		"agent-unrelated": map[string]interface{}{
			"scopes": []interface{}{"chat.only", "memory.read"},
		},
	}
	bu, _ := json.Marshal(grantUnrelated)
	os.WriteFile(grantFile, bu, 0600)
	errUn := canCreateProposal("agent-unrelated", nil)
	if errUn == nil || !strings.Contains(errUn.Error(), "ERR_PERMISSION_DENIED") {
		t.Errorf("grant with only unrelated scopes must still deny proposal.create; got: %v", errUn)
	}
	os.Remove(grantFile)
}

// TestProposalCreatePermissionAndAudit drives the REAL extracted handler (performProposalCreate)
// + canCreate gate + the post-switch audit append logic using withTempDir for files.
// Asserts side effects: saveToFile (proposals.json), scribe encode attempt, and audit append for
// both success and denied attempts. This exercises the shipped code paths, not a reimplementation.
func TestProposalCreatePermissionAndAudit(t *testing.T) {
	withTempDir(t, func() {
		proposals := make(map[string]interface{})
		var auditLog []interface{}
		// fake encoder to capture scribe send side effect
		var sentScribe []Message
		fakeEnc := json.NewEncoder(&fakeWriter{&sentScribe})

		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		_ = pub

		// denied via real gate + drive the *exact* post-switch audit block (appendAuditForStateChangeIfNeeded)
		// using the real shipped function (called by the loop for proposal.* even on error responses).
		srcLow := "microvm-low"
		pd := map[string]interface{}{"id": "denied-prop-1", "description": "x"}
		if err := canCreateProposal(srcLow, pd); err == nil || !strings.Contains(err.Error(), "ERR_PERMISSION_DENIED") {
			t.Fatalf("expected denied, got %v", err)
		}
		deniedMsg := Message{Source: srcLow, Command: "proposal.create", Timestamp: time.Now().Format(time.RFC3339)}
		deniedResp := Message{Command: "error", Payload: "ERR_PERMISSION_DENIED: ...", Timestamp: deniedMsg.Timestamp}
		appendAuditForStateChangeIfNeeded(deniedMsg, &deniedResp, &auditLog)
		saveAuditToFile("audit.json", auditLog) // ensure file written as the block does
		aud := loadAuditFromFile("audit.json")
		if len(aud) == 0 || !strings.Contains(fmt.Sprintf("%v", aud[len(aud)-1]), srcLow) {
			t.Error("denied attempt audit not appended via the *real* post-switch audit block")
		}

		// happy via REAL performProposalCreate (the shipped func called by the switch)
		srcHi := "client"
		ph := map[string]interface{}{"id": "happy-real-99", "description": "real handler skill"}
		if err := canCreateProposal(srcHi, ph); err != nil {
			t.Fatalf("privileged allow: %v", err)
		}
		id := ph["id"].(string)
		respP, sent := performProposalCreate(id, ph, proposals, fakeEnc, priv, time.Now().Format(time.RFC3339))
		if !sent {
			t.Error("perform must have attempted scribe.Encode (Court routing)")
		}
		if m, ok := respP.(map[string]interface{}); !ok || m["proposal_id"] != id {
			t.Errorf("real perform response must contain proposal_id; got %v", respP)
		}
		if proposals[id] == nil {
			t.Error("real performProposalCreate did not save to proposals map + file")
		}
		// check file side effect
		onDisk := loadFromFile("proposals.json")
		if onDisk[id] == nil {
			t.Error("proposals.json not written by real perform path")
		}

		// Also drive the *exact real post-switch audit block* for a success response (after perform).
		successMsg := Message{Source: srcHi, Command: "proposal.create", Timestamp: time.Now().Format(time.RFC3339)}
		successResp := Message{Command: "proposal.created", Payload: respP, Timestamp: successMsg.Timestamp}
		appendAuditForStateChangeIfNeeded(successMsg, &successResp, &auditLog)
		saveAuditToFile("audit.json", auditLog)
		aud2 := loadAuditFromFile("audit.json")
		found := false
		for _, e := range aud2 {
			if strings.Contains(fmt.Sprintf("%v", e), id) {
				found = true
				break
			}
			if m, ok := e.(map[string]interface{}); ok && m["source"] == srcHi {
				found = true
				break
			}
		}
		if !found {
			t.Error("success audit entry from real post-switch block not present")
		}
	})
}

// TestHandleProposalCreate is the authoritative table-driven test per the restructure.
// It calls ONLY the single orchestrator handleProposalCreate (which is the code the switch case now runs)
// with in-memory state. Covers happy, denied (no grant), denied (unrelated grant), and proper grant.
// Asserts response, Store proposals presence/absence, audit entries, and scribe side-effect.
func TestHandleProposalCreate(t *testing.T) {
	withTempDir(t, func() {
		type row struct {
			name       string
			source     string
			payload    map[string]interface{}
			grant      map[string]interface{} // optional grant to write
			wantCmd    string
			wantID     bool // whether proposals should contain the id after
			wantAudit  bool
			wantErrStr string
		}
		rows := []row{
			{
				name:    "privileged-client-happy",
				source:  "client",
				payload: map[string]interface{}{"id": "h1", "description": "happy via handle"},
				wantCmd: "proposal.created",
				wantID:  true,
				wantAudit: true,
			},
			{
				name:    "low-priv-no-grant",
				source:  "agent-low-no",
				payload: map[string]interface{}{"id": "d1", "description": "should deny"},
				wantCmd: "error",
				wantID:  false,
				wantAudit: true,
				wantErrStr: "ERR_PERMISSION_DENIED",
			},
			{
				name:    "low-priv-unrelated-grant",
				source:  "agent-chat-only",
				payload: map[string]interface{}{"id": "d2", "description": "unrelated grant"},
				grant: map[string]interface{}{
					"agent-chat-only": map[string]interface{}{"scopes": []interface{}{"chat.only"}},
				},
				wantCmd: "error",
				wantID:  false,
				wantAudit: true,
				wantErrStr: "ERR_PERMISSION_DENIED",
			},
			{
				name:    "low-priv-proper-grant",
				source:  "agent-proper",
				payload: map[string]interface{}{"id": "h2", "description": "proper grant"},
				grant: map[string]interface{}{
					"agent-proper": map[string]interface{}{"scopes": []interface{}{"proposal.create"}},
				},
				wantCmd: "proposal.created",
				wantID:  true,
				wantAudit: true,
			},
		}

		for _, r := range rows {
			t.Run(r.name, func(t *testing.T) {
				proposals := make(map[string]interface{})
				var auditLog []interface{}
				var sentScribe []Message
				fakeEnc := json.NewEncoder(&fakeWriter{&sentScribe})

				if r.grant != nil {
					b, _ := json.Marshal(r.grant)
					os.WriteFile("grants.json", b, 0600)
				} else {
					os.Remove("grants.json")
				}

				msg := Message{
					Source: r.source,
					Command: "proposal.create",
					Payload: r.payload,
					Timestamp: time.Now().Format(time.RFC3339),
				}
				_, priv, _ := ed25519.GenerateKey(rand.Reader)
				handled := handleProposalCreate(msg, proposals, fakeEnc, priv, &auditLog, msg.Timestamp)

				// Simulate the real loop's post-switch append (the single place that does audit for proposal.*)
				// This exercises the actual appendAuditForStateChangeIfNeeded code path that the live store uses.
				appendAuditForStateChangeIfNeeded(msg, &handled, &auditLog)

				if handled.Command != r.wantCmd {
					t.Errorf("want cmd %s, got %s (payload %v)", r.wantCmd, handled.Command, handled.Payload)
				}
				if r.wantErrStr != "" {
					s := ""
					switch v := handled.Payload.(type) {
					case string:
						s = v
					case map[string]interface{}:
						if res, ok := v["result"].(string); ok {
							s = res
						} else if e, ok := v["error"].(string); ok {
							s = e
						} else {
							s = fmt.Sprintf("%v", v)
						}
					default:
						s = fmt.Sprintf("%v", v)
					}
					if !strings.Contains(s, r.wantErrStr) {
						t.Errorf("expected err containing %s, got %v", r.wantErrStr, handled.Payload)
					}
				}
				_, hasID := proposals[r.payload["id"].(string)]
				if hasID != r.wantID {
					t.Errorf("proposals has id=%v want=%v", hasID, r.wantID)
				}
				if r.wantAudit && len(auditLog) == 0 {
					t.Error("expected audit entry from handle (case) + real post-switch append")
				}
				// Hard assert for scribe.notify_review side-effect on happy path (per verification plan)
				if r.wantCmd == "proposal.created" {
					if len(sentScribe) == 0 {
						t.Error("expected scribe.notify_review to be sent for happy create")
					} else {
						if sentScribe[0].Command != "scribe.notify_review" {
							t.Errorf("expected scribe.notify_review, got %s", sentScribe[0].Command)
						}
						if p, ok := sentScribe[0].Payload.(map[string]interface{}); !ok || p["proposal_id"] == nil {
							t.Error("scribe payload must contain proposal_id")
						}
					}
				}
			})
		}
	})
}
