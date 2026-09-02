package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// T1–T13 drive live runStore over a fake Hub unix socket. They must fail on
// the current JSON-dashboard / stub-git.push Store. No t.Skip. No source greps.

type liveHub struct {
	t    *testing.T
	conn net.Conn
	ln   net.Listener
	enc  *json.Encoder
	dec  *json.Decoder
	mu   sync.Mutex
}

func withLiveStore(t *testing.T, fn func(h *liveHub)) {
	t.Helper()
	withTempDir(t, func() {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		sock := filepath.Join(cwd, "hub.sock")
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatalf("listen hub: %v", err)
		}

		prev := hubSocket
		hubSocket = sock
		t.Cleanup(func() {
			hubSocket = prev
			_ = ln.Close()
		})

		type acc struct {
			c   net.Conn
			err error
		}
		accCh := make(chan acc, 1)
		go func() {
			c, err := ln.Accept()
			accCh <- acc{c, err}
		}()
		go runStore(nil, nil)

		var got acc
		select {
		case got = <-accCh:
		case <-time.After(5 * time.Second):
			_ = ln.Close()
			t.Fatal("hub accept timeout: runStore did not dial unix socket")
		}
		if got.err != nil {
			t.Fatalf("hub accept: %v", got.err)
		}

		h := &liveHub{
			t:    t,
			conn: got.c,
			ln:   ln,
			enc:  json.NewEncoder(got.c),
			dec:  json.NewDecoder(got.c),
		}
		t.Cleanup(func() {
			// Leave the accepted conn open so runStore blocks on Decode.
			// Closing it makes Decode return EOF in a tight continue-loop
			// and floods the test log.
			_ = ln.Close()
		})

		_ = h.conn.SetDeadline(time.Now().Add(5 * time.Second))
		var reg Message
		if err := h.dec.Decode(&reg); err != nil {
			t.Fatalf("register decode: %v", err)
		}
		if err := h.enc.Encode(map[string]interface{}{}); err != nil {
			t.Fatalf("register reply: %v", err)
		}
		_ = h.conn.SetDeadline(time.Time{})

		fn(h)
	})
}

func skipExtra(m Message) bool {
	// Skip unsolicited Store→peer frames, not responses whose Destination is
	// the caller (e.g. Source=builder git.push replies Destination=builder).
	switch m.Command {
	case "builder.build_proposal", "scribe.notify_review",
		"autonomy.expired", "background.expired", "timer.fired":
		return true
	}
	return false
}

func (h *liveHub) call(source, command string, payload interface{}) Message {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()

	req := Message{
		Source:      source,
		Destination: "store",
		Command:     command,
		Payload:     payload,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := h.enc.Encode(req); err != nil {
		h.t.Fatalf("encode %s: %v", command, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	_ = h.conn.SetReadDeadline(deadline)
	defer h.conn.SetReadDeadline(time.Time{})

	for {
		var m Message
		if err := h.dec.Decode(&m); err != nil {
			h.t.Fatalf("decode %s (source=%s): %v\nstore stderr:\n%s", command, source, err, h.stderr.String())
		}
		if m.Destination == source || (m.Source == "store" && m.Destination == "") {
			return m
		}
		if skipExtra(m) {
			continue
		}
	}
}

func payloadText(m Message) string {
	switch p := m.Payload.(type) {
	case nil:
		return ""
	case string:
		return p
	case map[string]interface{}:
		if s, ok := p["result"].(string); ok {
			return s
		}
		if s, ok := p["error"].(string); ok {
			return s
		}
		b, _ := json.Marshal(p)
		return string(b)
	default:
		b, _ := json.Marshal(p)
		return string(b)
	}
}

func payloadMap(m Message) map[string]interface{} {
	p, ok := m.Payload.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	if inner, ok := p["result"].(map[string]interface{}); ok {
		return inner
	}
	return p
}

// isMissingHandler reports empty Command / unknown-command / default capability
// gate. Those are not a real deny from a merge/git handler.
func isMissingHandler(m Message) bool {
	if strings.TrimSpace(m.Command) == "" {
		return true
	}
	if m.Command != "error" {
		return false
	}
	s := strings.TrimSpace(payloadText(m))
	low := strings.ToLower(s)
	if low == "unknown command" || strings.Contains(low, "unknown command") {
		return true
	}
	if s == "ERR_PERMISSION_DENIED" || low == "err_permission_denied" {
		return true
	}
	return false
}

func isErrorDeny(m Message) bool {
	if isMissingHandler(m) {
		return false
	}
	if m.Command == "error" {
		return true
	}
	if p, ok := m.Payload.(map[string]interface{}); ok {
		if _, ok := p["error"]; ok {
			return true
		}
		if v, ok := p["ok"].(bool); ok && !v {
			return true
		}
	}
	low := strings.ToLower(payloadText(m))
	if strings.Contains(low, "denied") || strings.Contains(low, "forbidden") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(payloadText(m))), "err_") {
		return true
	}
	return false
}

func isSuccess(m Message) bool {
	if isMissingHandler(m) || isErrorDeny(m) {
		return false
	}
	if strings.EqualFold(m.Command, "error") {
		return false
	}
	return m.Command != ""
}

func observeGitDirs(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.IsDir() {
			return nil
		}
		switch info.Name() {
		case "objects", "refs", "hooks", "info", "branches", "logs":
			return filepath.SkipDir
		}
		head := filepath.Join(path, "HEAD")
		objects := filepath.Join(path, "objects")
		st1, err1 := os.Stat(head)
		st2, err2 := os.Stat(objects)
		if err1 == nil && !st1.IsDir() && err2 == nil && st2.IsDir() {
			out = append(out, path)
			return filepath.SkipDir
		}
		return nil
	})
	return out
}

func remoteFrom(m Message) string {
	switch p := m.Payload.(type) {
	case string:
		s := strings.TrimSpace(p)
		if s == "" || strings.EqualFold(s, "ok") {
			return ""
		}
		return s
	case map[string]interface{}:
		if inner, ok := p["result"]; ok {
			if s, ok := inner.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" && !strings.EqualFold(s, "ok") {
					return s
				}
			}
			if mm, ok := inner.(map[string]interface{}); ok {
				p = mm
			}
		}
		for _, k := range []string{"remote", "url", "clone_url", "git_url", "vsock", "hub", "path"} {
			if s, ok := p[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func isLocalGitTransport(remote string) bool {
	r := strings.ToLower(remote)
	if strings.HasPrefix(r, "file:") || strings.HasPrefix(r, "git://") {
		return true
	}
	if strings.Contains(r, "://") {
		if strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") ||
			strings.HasPrefix(r, "ssh://") || strings.HasPrefix(r, "tcp://") {
			return true
		}
		return false
	}
	// bare path on disk
	if strings.HasPrefix(remote, "/") || strings.HasPrefix(remote, "repos/") || strings.HasPrefix(remote, "./") {
		return true
	}
	return false
}

func leftoverCreds(root string) []string {
	var hits []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		n := info.Name()
		switch n {
		case ".git-credentials", ".netrc", "id_rsa", "id_ed25519", "credentials":
			hits = append(hits, path)
		default:
			if strings.HasSuffix(n, ".pem") {
				hits = append(hits, path)
			}
		}
		return nil
	})
	return hits
}

func TestT1_MergeWithoutCourtMustFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		created := h.call("client", "pr.create", map[string]interface{}{
			"id":    "pr-t1",
			"title": "merge without court",
			"from":  "feature",
			"to":    "main",
		})
		if isErrorDeny(created) && !isSuccess(created) && created.Command != "pr.created" {
			// still try merge; create failing is not a pass for T1
		}
		merged := h.call("client", "pr.merge", map[string]interface{}{
			"id": "pr-t1",
		})
		if isMissingHandler(merged) {
			t.Fatal("T1: pr.merge missing handler is not a deny; merge-without-Court is not fail-closed")
		}
		if isSuccess(merged) {
			t.Fatal("T1: pr.merge succeeded without Court approval")
		}
		if !isErrorDeny(merged) {
			t.Fatalf("T1: merge-without-Court must be an error deny, got command=%q payload=%s", merged.Command, payloadText(merged))
		}
	})
}

func TestT2_CourtSkipMustNotExist(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("client", "proposal.create", map[string]interface{}{
			"id":          "prop-t2",
			"description": "court skip probe",
		})
		_ = h.call("court-scribe", "court.review_complete", map[string]interface{}{
			"proposal_id": "prop-t2",
			"votes":       map[string]interface{}{"persona-a": "yes", "persona-b": "yes"},
			// approved omitted — current Store fallback treats this as approve
		})
		got := h.call("client", "proposal.get", map[string]interface{}{"id": "prop-t2"})
		st, _ := payloadMap(got)["state"].(string)
		if strings.EqualFold(st, "approved") {
			t.Fatal("T2: court.review_complete without approved was treated as approve (skip under another name)")
		}

		_ = h.call("client", "pr.create", map[string]interface{}{
			"id":         "pr-t2-skip",
			"skipped":    true,
			"skip_court": true,
		})
		skipMerge := h.call("client", "pr.merge", map[string]interface{}{
			"id":         "pr-t2-skip",
			"skipped":    true,
			"skip_court": true,
		})
		if isSuccess(skipMerge) {
			t.Fatal("T2: skip_court/skipped merge succeeded")
		}
		if isMissingHandler(skipMerge) && !strings.EqualFold(st, "approved") {
			t.Fatal("T2: Store does not refuse court skip (missing merge handler is not fail-closed)")
		}
	})
}

func TestT3_CrossTenantFetchMustFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("tenant-a", "git.clone", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-a",
		})
		pathsA := observeGitDirs(".")
		_ = h.call("tenant-b", "git.clone", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-b",
		})
		pathsB := observeGitDirs(".")
		if len(pathsA) == 0 {
			t.Fatal("T3: git.clone as tenant-a created no git repo to observe")
		}
		if len(pathsA) == 1 && len(pathsB) == 1 && pathsA[0] == pathsB[0] {
			t.Fatalf("T3: tenant-a and tenant-b both landed on %s (shared repo, no ACL)", pathsA[0])
		}
		same := false
		for _, a := range pathsA {
			for _, b := range pathsB {
				if a == b {
					same = true
				}
			}
		}
		if same && len(pathsB) <= len(pathsA) {
			t.Fatalf("T3: tenants share store path/refs A=%v B=%v", pathsA, pathsB)
		}

		// B must not be able to fetch A's objects from a shared store.
		if len(pathsA) > 0 && len(pathsB) > 0 && pathsA[0] != pathsB[0] {
			blob := []byte("tenant-a-secret-object")
			cmd := exec.Command("git", "-C", pathsA[0], "hash-object", "-w", "--stdin")
			cmd.Stdin = strings.NewReader(string(blob))
			out, err := cmd.CombinedOutput()
			if err == nil {
				hash := strings.TrimSpace(string(out))
				fetch := exec.Command("git", "-C", pathsB[0], "fetch", pathsA[0])
				_ = fetch.Run()
				cat := exec.Command("git", "-C", pathsB[0], "cat-file", "-t", hash)
				if cat.Run() == nil {
					t.Fatal("T3: tenant-b can fetch tenant-a's objects")
				}
			}
		}
	})
}

func TestT4_TenantACannotPushToB(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("tenant-b", "git.clone", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-b",
		})
		paths := observeGitDirs(".")
		target := "skill"
		if len(paths) > 0 {
			target = paths[0]
		}
		pushed := h.call("tenant-a", "git.push", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-b",
			"remote": target,
			"target": "tenant-b",
		})
		if isSuccess(pushed) || !isErrorDeny(pushed) {
			t.Fatalf("T4: tenant-a git.push into tenant-b's repo was not an error (command=%q payload=%s)", pushed.Command, payloadText(pushed))
		}
	})
}

func TestT5_ExtraRemoteSubmoduleLFSMustFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		pushed := h.call("builder", "git.push", map[string]interface{}{
			"repo":       "skill",
			"remotes":    []string{"https://evil.example/extra.git"},
			"remote":     "https://evil.example/extra.git",
			"submodule":  map[string]string{"path": "vendor/x", "url": "https://evil.example/x.git"},
			"lfs":        true,
			"gitmodules": true,
		})
		if isSuccess(pushed) || !isErrorDeny(pushed) {
			t.Fatalf("T5: extra remote/submodule/LFS git.push was not rejected (command=%q payload=%s)", pushed.Command, payloadText(pushed))
		}
		cloned := h.call("builder", "git.clone", map[string]interface{}{
			"repo":       "skill-extra",
			"remote":     "https://evil.example/extra.git",
			"submodule":  true,
			"lfs":        true,
			"gitmodules": true,
		})
		if isSuccess(cloned) && !isErrorDeny(cloned) {
			t.Fatal("T5: git.clone with extra remote/submodule/LFS was accepted")
		}
	})
}

func TestT6_CoderHasNoGit(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		cloned := h.call("coder", "git.clone", map[string]interface{}{"repo": "skill"})
		pushed := h.call("coder", "git.push", map[string]interface{}{"repo": "skill"})
		if isSuccess(cloned) || !isErrorDeny(cloned) {
			t.Fatalf("T6: git.clone as coder was allowed (command=%q payload=%s)", cloned.Command, payloadText(cloned))
		}
		if isSuccess(pushed) || !isErrorDeny(pushed) {
			t.Fatalf("T6: git.push as coder was allowed (command=%q payload=%s)", pushed.Command, payloadText(pushed))
		}
	})
}

func TestT7_RealGitClonePush(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		cloned := h.call("tenant-a", "git.clone", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-a",
		})
		remote := remoteFrom(cloned)
		if remote == "" {
			t.Fatal("T7: git.clone did not return a cloneable remote; init --bare + stub push is not real git")
		}
		if isLocalGitTransport(remote) {
			t.Fatalf("T7: Store remote %q is local git init --bare / file, not a remote that Store mediates", remote)
		}
		work := t.TempDir()
		dst := filepath.Join(work, "clone")
		out, err := exec.Command("git", "clone", remote, dst).CombinedOutput()
		if err != nil {
			t.Fatalf("T7: git clone of Store remote failed (not a real remote): %v\n%s", err, out)
		}
		_ = exec.Command("git", "-C", dst, "config", "user.email", "t7@example.com").Run()
		_ = exec.Command("git", "-C", dst, "config", "user.name", "T7").Run()
		if err := os.WriteFile(filepath.Join(dst, "t7.txt"), []byte("t7"), 0644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dst, "add", "t7.txt").CombinedOutput(); err != nil {
			t.Fatalf("T7: git add: %v\n%s", err, out)
		}
		if out, err := exec.Command("git", "-C", dst, "commit", "-m", "t7").CombinedOutput(); err != nil {
			t.Fatalf("T7: git commit: %v\n%s", err, out)
		}
		if out, err := exec.Command("git", "-C", dst, "push", "origin", "HEAD").CombinedOutput(); err != nil {
			t.Fatalf("T7: git push to Store remote failed (stub push / not a real remote): %v\n%s", err, out)
		}
		rpc := h.call("tenant-a", "git.push", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-a",
			"ref":    "HEAD",
		})
		if !isSuccess(rpc) && isErrorDeny(rpc) {
			t.Fatal("T7: git.push RPC rejected a real object push")
		}
	})
}

func TestT8_PrMergeOnlyStore(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("client", "pr.create", map[string]interface{}{
			"id":    "pr-t8",
			"title": "only store may merge",
		})
		for _, src := range []string{"coder", "builder", "host-git", "git"} {
			merged := h.call(src, "pr.merge", map[string]interface{}{"id": "pr-t8"})
			if isMissingHandler(merged) {
				t.Fatalf("T8: pr.merge as %s: missing handler is not a deny", src)
			}
			if isSuccess(merged) {
				t.Fatalf("T8: pr.merge as %s succeeded; only Store may merge", src)
			}
			if !isErrorDeny(merged) {
				t.Fatalf("T8: pr.merge as %s must be an error deny, got command=%q payload=%s", src, merged.Command, payloadText(merged))
			}
		}
	})
}

func TestT9_NewSkillAlwaysStoreRepo(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("client", "git.create", map[string]interface{}{
			"repo":  "newskill",
			"skill": "newskill",
		})
		_ = h.call("client", "skill.create", map[string]interface{}{
			"id":   "newskill",
			"name": "newskill",
		})
		_ = h.call("client", "skill.register", map[string]interface{}{
			"id":   "newskill",
			"name": "newskill",
		})
		dirs := observeGitDirs(".")
		cloneable := false
		remoteOK := ""
		for _, d := range dirs {
			tmp := t.TempDir()
			if out, err := exec.Command("git", "clone", d, filepath.Join(tmp, "c")).CombinedOutput(); err == nil {
				cloneable = true
				remoteOK = d
				_ = out
			}
		}
		if !cloneable {
			t.Fatal("T9: creating a skill did not allocate a cloneable Store git repo")
		}
		_ = remoteOK
	})
}

func TestT10_RollbackOpensNewPR(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("client", "pr.create", map[string]interface{}{
			"id":    "pr-t10",
			"title": "original",
		})
		rb := h.call("client", "pr.rollback", map[string]interface{}{
			"id": "pr-t10",
		})
		if isMissingHandler(rb) {
			t.Fatal("T10: no pr.rollback that opens a new Court-required PR")
		}
		newID := ""
		pm := payloadMap(rb)
		for _, k := range []string{"id", "pr_id", "new_pr", "new_id", "rollback_pr"} {
			if s, ok := pm[k].(string); ok && s != "" && s != "pr-t10" {
				newID = s
			}
		}
		if s := strings.TrimSpace(payloadText(rb)); newID == "" && s != "" && s != "ok" && !isErrorDeny(rb) {
			if s != "pr-t10" {
				newID = s
			}
		}
		if newID == "" {
			t.Fatal("T10: pr.rollback did not return a new PR id")
		}
		got := h.call("client", "pr.get", map[string]interface{}{"id": newID})
		st, _ := payloadMap(got)["state"].(string)
		skip, _ := payloadMap(got)["skip_court"].(bool)
		if skip || strings.EqualFold(st, "merged") || strings.EqualFold(st, "approved") {
			t.Fatal("T10: rollback PR skipped Court / live skip-Court reset")
		}
		if isErrorDeny(got) || isMissingHandler(got) {
			t.Fatal("T10: new rollback PR id is not gettable")
		}
	})
}

func TestT11_DestroyedBuilderLeavesNoState(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.call("tenant-a", "git.clone", map[string]interface{}{"repo": "skill"})
		cmds := []string{"builder.destroy", "destroy.builder", "builder.destroyed", "store.builder.destroy"}
		handled := false
		for _, c := range cmds {
			m := h.call("store", c, map[string]interface{}{"builder_id": "builder-1"})
			if !isMissingHandler(m) {
				handled = true
			}
		}
		if !handled {
			t.Fatal("T11: Store has no Builder destroy that wipes git state and creds")
		}
		left := observeGitDirs(".")
		creds := leftoverCreds(".")
		if len(left) > 0 || len(creds) > 0 {
			t.Fatalf("T11: leftover git/creds after destroy: git=%v creds=%v", left, creds)
		}
	})
}

func TestT12_ForcePushHistoryDeleteFakeCourtFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		fp := h.call("builder", "git.push", map[string]interface{}{
			"repo":        "skill",
			"force":       true,
			"delete_refs": true,
			"refs":        []string{"+refs/heads/main", ":refs/heads/old"},
		})
		if isSuccess(fp) || !isErrorDeny(fp) {
			t.Fatalf("T12: force-push / delete-refs was not denied (command=%q payload=%s)", fp.Command, payloadText(fp))
		}

		_ = h.call("client", "proposal.create", map[string]interface{}{
			"id":          "prop-t12",
			"description": "fake court",
		})
		_ = h.call("court-scribe", "court.review_complete", map[string]interface{}{
			"proposal_id": "prop-t12",
			"votes":       map[string]interface{}{"a": "yes"},
			"approved":    true,
			// no decision_merkle / decision_sig
		})
		got := h.call("client", "proposal.get", map[string]interface{}{"id": "prop-t12"})
		pm := payloadMap(got)
		st, _ := pm["state"].(string)
		_, hasMerkle := pm["court_decision"]
		if strings.EqualFold(st, "approved") && !hasMerkle {
			t.Fatal("T12: fake Court (approved without decision_merkle/sig) was accepted")
		}
		_ = h.call("client", "pr.create", map[string]interface{}{"id": "pr-t12"})
		merged := h.call("client", "pr.merge", map[string]interface{}{"id": "pr-t12", "proposal_id": "prop-t12"})
		if isSuccess(merged) {
			t.Fatal("T12: pr.merge after forged/unsigned Court succeeded")
		}
		if isMissingHandler(merged) && strings.EqualFold(st, "approved") {
			t.Fatal("T12: Store accepted unsigned Court approval")
		}
	})
}

func TestT13_HubVsockOnlyNoHostGitDaemonNoSkillDotGit(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		cloned := h.call("tenant-a", "git.clone", map[string]interface{}{
			"repo": "skill",
		})
		remote := remoteFrom(cloned)
		if isLocalGitTransport(remote) {
			t.Fatalf("T13: Store returned local/file/TCP git transport %q, not Hub/vsock", remote)
		}
		dirs := observeGitDirs(".")
		for _, d := range dirs {
			rel := d
			if strings.Contains(filepath.ToSlash(rel), "/repos/") || strings.HasPrefix(filepath.ToSlash(rel), "repos/") || rel == "repos/skill" {
				t.Fatalf("T13: Store used local git init --bare at %s, not Hub/vsock git", d)
			}
			if strings.HasPrefix(d, "/") && !strings.Contains(strings.ToLower(d), "vsock") && !strings.Contains(strings.ToLower(d), "hub") {
				// local absolute bare repo
				t.Fatalf("T13: Store used local filesystem git at %s, not Hub/vsock git", d)
			}
		}
		if len(dirs) > 0 && (remote == "" || isLocalGitTransport(remote)) {
			t.Fatalf("T13: git.clone created local git dirs %v (init --bare), not Hub/vsock", dirs)
		}

		// Extra: workspace/skills/*/.git (sitting). Not the primary assertion.
		_ = filepath.Walk("workspace", func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() && info.Name() == ".git" {
				t.Fatal("T13: workspace/skills has a .git dir (host git, not Hub/vsock)")
			}
			return nil
		})

		// Extra: host git daemon listen (port 9418). Fail if present.
		if gitDaemonListening() {
			t.Fatal("T13: host git daemon is listening; git must be Hub/vsock only")
		}

		// Pass only when Store returned a Hub/vsock remote and did not
		// materialize a local git dir. Missing vsock stays red even if
		// someone deletes git init --bare.
		if remote == "" || isLocalGitTransport(remote) || len(dirs) > 0 {
			t.Fatalf("T13: git.clone is not Hub/vsock git (remote=%q localDirs=%v command=%q)", remote, dirs, cloned.Command)
		}
	})
}

func gitDaemonListening() bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:9418", 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
