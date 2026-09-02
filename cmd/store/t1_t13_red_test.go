package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var storeBin string

func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "aegis-store-t1t13-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	storeBin = filepath.Join(dir, "store")
	cmd := exec.Command("go", "build", "-o", storeBin, ".")
	cmd.Dir = wd
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build store: %v\n%s\n", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// T1-T13 drive live runStore over a fake Hub unix socket.
// They must FAIL on the current JSON-dashboard / stub-git Store.
// Unknown command is not a deny. No t.Skip. No t.Parallel.

const rpcTimeout = 3 * time.Second

var (
	packageCWD     string
	gitWorktree    string
	liveStoreConns []net.Conn
)

func init() {
	wd, err := os.Getwd()
	if err == nil {
		packageCWD = wd
		gitWorktree = findGitWorktree(wd)
	}
}

func findGitWorktree(start string) string {
	dir := start
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return start
}

// handledDeny is true only when Store handled the command and refused for a
// spec reason. Empty Command, unknown-command, and stub success are NOT denies.
func handledDeny(resp Message, substrings ...string) bool {
	if resp.Command == "" || resp.Command == "pr.merged" || resp.Command == "git.pushed" || resp.Command == "ok" {
		return false
	}
	p := strings.ToLower(fmt.Sprint(resp.Payload))
	if strings.Contains(p, "unknown") {
		return false
	}
	if resp.Command != "error" {
		return false
	}
	for _, s := range substrings {
		if strings.Contains(p, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func payloadText(resp Message) string {
	return strings.ToLower(fmt.Sprint(resp.Payload))
}

func isUnknownOrEmpty(resp Message) bool {
	if resp.Command == "" {
		return true
	}
	return strings.Contains(payloadText(resp), "unknown")
}

func looksLikeHubVsock(v interface{}) bool {
	t := strings.ToLower(fmt.Sprint(v))
	if t == "" || t == "ok" || t == "<nil>" {
		return false
	}
	if strings.HasPrefix(t, "/") || strings.HasPrefix(t, "file:") || strings.Contains(t, "/repos/") {
		return false
	}
	return strings.Contains(t, "vsock") || strings.Contains(t, "hub")
}

func isStubGitPushed(resp Message) bool {
	if resp.Command != "git.pushed" && resp.Command != "ok" {
		return false
	}
	p := strings.TrimSpace(payloadText(resp))
	return p == "ok" || p == ""
}

type liveHub struct {
	t    *testing.T
	ln   net.Listener
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
	cwd  string
	cmd  *exec.Cmd
}

func startLiveHub(t *testing.T) *liveHub {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "hub.sock")
	if !filepath.IsAbs(sock) || len(sock) <= 2 {
		t.Fatalf("unix socket path must be absolute and longer than ~/ (got %q)", sock)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen fake hub: %v", err)
	}
	cmd := exec.Command(storeBin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+sock)
	cmd.Stderr = io.Discard
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		_ = ln.Close()
		t.Fatalf("start store: %v", err)
	}
	h := &liveHub{t: t, ln: ln, cwd: dir, cmd: cmd}
	t.Cleanup(func() {
		if h.cmd != nil && h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
			_, _ = h.cmd.Process.Wait()
		}
		if h.conn != nil {
			_ = h.conn.Close()
		}
		_ = ln.Close()
	})
	if ul, ok := ln.(*net.UnixListener); ok {
		_ = ul.SetDeadline(time.Now().Add(10 * time.Second))
	}
	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept store dial (listener was up first): %v", err)
	}
	h.conn = conn
	h.enc = json.NewEncoder(conn)
	h.dec = json.NewDecoder(conn)
	h.handshake()
	return h
}

func (h *liveHub) handshake() {
	h.t.Helper()
	_ = h.conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = h.conn.SetDeadline(time.Time{}) }()
	var reg Message
	if err := h.dec.Decode(&reg); err != nil {
		h.t.Fatalf("decode register Message: %v", err)
	}
	if err := h.enc.Encode(map[string]interface{}{}); err != nil {
		h.t.Fatalf("encode register reply: %v", err)
	}
}

func (h *liveHub) rpc(source, command string, payload interface{}) Message {
	h.t.Helper()
	_ = h.conn.SetDeadline(time.Now().Add(rpcTimeout))
	defer func() { _ = h.conn.SetDeadline(time.Time{}) }()
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
	for {
		var resp Message
		if err := h.dec.Decode(&resp); err != nil {
			h.t.Fatalf("decode reply to %s: %v", command, err)
		}
		if resp.Destination == source {
			return resp
		}
	}
}

func withLiveStore(t *testing.T, fn func(h *liveHub)) {
	t.Helper()
	h := startLiveHub(t)
	fn(h)
}

func listGitRepos(root string) []string {
	var found []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.IsDir() {
			return nil
		}
		if filepath.Base(path) == ".git" {
			found = append(found, path)
			return filepath.SkipDir
		}
		if gitDirLooksBare(path) {
			found = append(found, path)
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func gitDirLooksBare(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "objects")); err != nil {
		return false
	}
	base := filepath.Base(path)
	if base == "objects" || base == "refs" {
		return false
	}
	return true
}

func absRepos(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			a = p
		}
		a = filepath.Clean(a)
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

func addedPaths(before, after []string) []string {
	bset := map[string]bool{}
	for _, p := range absRepos(before) {
		bset[p] = true
	}
	var add []string
	for _, p := range absRepos(after) {
		if !bset[p] {
			add = append(add, p)
		}
	}
	return add
}

func worktreeReposLeftover() []string {
	var hits []string
	for _, root := range []string{gitWorktree, packageCWD} {
		if root == "" {
			continue
		}
		p := filepath.Join(root, "repos")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			hits = append(hits, p)
		}
	}
	return hits
}

func gitDaemonListening() []string {
	var hit []string
	for _, addr := range []string{"127.0.0.1:9418", "[::1]:9418"} {
		c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = c.Close()
			hit = append(hit, addr)
		}
	}
	return hit
}

func hostSkillDotGit(root string) []string {
	var hits []string
	ws := filepath.Join(root, "workspace", "skills")
	_ = filepath.Walk(ws, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() && filepath.Base(path) == ".git" {
			hits = append(hits, path)
			return filepath.SkipDir
		}
		return nil
	})
	return hits
}

func clonePayload(tenant, repo string) map[string]interface{} {
	return map[string]interface{}{"repo": repo, "tenant": tenant}
}

func TestT1_MergeWithoutCourtMustFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "pr.create", map[string]interface{}{
			"id":     "pr-t1",
			"repo":   "skill",
			"tenant": "tenant-a",
			"title":  "merge without court",
		})
		resp := h.rpc("store", "pr.merge", map[string]interface{}{
			"id":     "pr-t1",
			"repo":   "skill",
			"tenant": "tenant-a",
		})
		if !handledDeny(resp, "court", "not approved") {
			t.Fatalf("T1: pr.merge must be handled and refuse because Court is not approved (got cmd=%q payload=%v); unknown/empty/success is not a deny", resp.Command, resp.Payload)
		}
	})
}

func TestT2_CourtSkipMustNotExist(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		created := h.rpc("client", "proposal.create", map[string]interface{}{
			"id":          "prop-t2",
			"description": "court skip via omitted approved field",
		})
		if created.Command != "proposal.created" {
			t.Fatalf("T2: proposal.create failed cmd=%q payload=%v", created.Command, created.Payload)
		}
		_ = h.rpc("court-scribe", "court.review_complete", map[string]interface{}{
			"proposal_id": "prop-t2",
			"votes": map[string]interface{}{
				"ciso":   "approve",
				"lawyer": "approve",
			},
		})
		got := h.rpc("client", "proposal.get", map[string]interface{}{"id": "prop-t2"})
		text := payloadText(got)
		if strings.Contains(text, "approved") || strings.Contains(text, "mergeable") {
			t.Fatalf("T2: Store marked proposal approved/mergeable without approved field (court skip fallback): cmd=%q payload=%v", got.Command, got.Payload)
		}
	})
}

func TestT3_CrossTenantFetchMustFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		before := listGitRepos(h.cwd)
		aResp := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		afterA := listGitRepos(h.cwd)
		bResp := h.rpc("builder", "git.clone", clonePayload("tenant-b", "skill"))
		afterB := listGitRepos(h.cwd)
		pathsA := addedPaths(before, afterA)
		pathsB := addedPaths(afterA, afterB)
		allA := absRepos(afterA)
		allB := absRepos(afterB)
		t.Logf("T3 clone A cmd=%q payload=%v new=%v", aResp.Command, aResp.Payload, pathsA)
		t.Logf("T3 clone B cmd=%q payload=%v new=%v", bResp.Command, bResp.Payload, pathsB)
		shared := false
		if len(pathsB) == 0 {
			shared = true
		}
		for _, pa := range allA {
			for _, pb := range allB {
				if pa == pb {
					shared = true
				}
			}
		}
		if shared {
			t.Fatalf("T3: tenants tenant-a and tenant-b both land on the same on-disk repo for skill (no tenant prefix); A=%v B=%v afterA=%v afterB=%v", pathsA, pathsB, allA, allB)
		}
	})
}

func TestT4_TenantACannotPushToB(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		_ = h.rpc("builder", "git.clone", clonePayload("tenant-b", "skill"))
		resp := h.rpc("tenant-a", "git.push", map[string]interface{}{
			"repo":          "skill",
			"tenant":        "tenant-a",
			"target_tenant": "tenant-b",
			"target_repo":   "skill",
		})
		if !handledDeny(resp, "tenant", "tenancy", "cross-tenant", "acl", "not your") {
			t.Fatalf("T4: tenant-a git.push targeting tenant-b skill must be a tenancy deny (got cmd=%q payload=%v); stub success/unknown is not a deny", resp.Command, resp.Payload)
		}
	})
}

func TestT5_ExtraRemoteSubmoduleLFSMustFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		resp := h.rpc("builder", "git.push", map[string]interface{}{
			"repo":         "skill",
			"tenant":       "tenant-a",
			"extra_remote": "https://evil.example/x.git",
			"submodule":    true,
			"gitmodules":   true,
			"lfs":          true,
			"hooks":        true,
		})
		if !handledDeny(resp, "remote", "submodule", "lfs", "hook") {
			t.Fatalf("T5: builder git.push with extra remote/submodule/LFS must be handled deny (got cmd=%q payload=%v)", resp.Command, resp.Payload)
		}
	})
}

func TestT6_CoderHasNoGit(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		cloneResp := h.rpc("coder", "git.clone", clonePayload("tenant-a", "skill"))
		pushResp := h.rpc("coder", "git.push", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-a",
		})
		cloneDenied := handledDeny(cloneResp, "coder", "actor", "no git", "not allowed", "denied")
		pushDenied := handledDeny(pushResp, "coder", "actor", "no git", "not allowed", "denied")
		if !cloneDenied || !pushDenied {
			t.Fatalf("T6: coder must not have git (clone cmd=%q payload=%v deny=%v; push cmd=%q payload=%v deny=%v)", cloneResp.Command, cloneResp.Payload, cloneDenied, pushResp.Command, pushResp.Payload, pushDenied)
		}
	})
}

func TestT7_RealGitClonePush(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		cloned := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		want := "t7-round-trip-commit"
		pushed := h.rpc("builder", "git.push", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-a",
			"ref":    "refs/heads/main",
			"commit": want,
		})
		if isStubGitPushed(pushed) {
			t.Fatal("T7: stub git.pushed is not a commit round-trip through Store git.push / git.clone RPCs")
		}
		cloned2 := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		blob := fmt.Sprint(cloned.Payload) + " " + fmt.Sprint(pushed.Payload) + " " + fmt.Sprint(cloned2.Payload)
		if strings.Contains(blob, "/repos/") || strings.HasPrefix(strings.TrimSpace(fmt.Sprint(cloned2.Payload)), "/") || strings.Contains(strings.ToLower(blob), "file:") {
			t.Fatalf("T7: Store remote is the local init --bare path; round-trip must go through Store git.push / git.clone RPCs, not git push into repos/skill (clone1=%v push=%v clone2=%v)", cloned.Payload, pushed.Payload, cloned2.Payload)
		}
		if !looksLikeHubVsock(cloned2.Payload) && !strings.Contains(strings.ToLower(blob), strings.ToLower(want)) {
			t.Fatalf("T7: commit did not round-trip through Store git.push / git.clone RPCs (push cmd=%q payload=%v clone2=%v)", pushed.Command, pushed.Payload, cloned2.Payload)
		}
	})
}

func TestT8_PrMergeOnlyStore(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "pr.create", map[string]interface{}{
			"id":     "pr-t8",
			"repo":   "skill",
			"tenant": "tenant-a",
		})
		coder := h.rpc("coder", "pr.merge", map[string]interface{}{"id": "pr-t8"})
		builder := h.rpc("builder", "pr.merge", map[string]interface{}{"id": "pr-t8"})
		if !handledDeny(coder, "wrong actor", "only store", "store only", "actor") {
			t.Fatalf("T8: pr.merge as coder must be handled wrong-actor deny (got cmd=%q payload=%v); missing case/unknown is not a deny", coder.Command, coder.Payload)
		}
		if !handledDeny(builder, "wrong actor", "only store", "store only", "actor") {
			t.Fatalf("T8: pr.merge as builder must be handled wrong-actor deny (got cmd=%q payload=%v); missing case/unknown is not a deny", builder.Command, builder.Payload)
		}
	})
}

func TestT9_NewSkillAlwaysStoreRepo(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		created := h.rpc("client", "git.create", map[string]interface{}{
			"repo":   "newskill",
			"tenant": "tenant-a",
		})
		reg := h.rpc("client", "skill.register", map[string]interface{}{
			"id":   "newskill",
			"name": "newskill",
		})
		skill := h.rpc("client", "skill.create", map[string]interface{}{
			"id":     "newskill",
			"repo":   "newskill",
			"tenant": "tenant-a",
		})
		blob := fmt.Sprint(created.Payload) + " " + fmt.Sprint(reg.Payload) + " " + fmt.Sprint(skill.Payload)
		local := absRepos(listGitRepos(h.cwd))
		hasRemote := looksLikeHubVsock(created.Payload) || looksLikeHubVsock(skill.Payload)
		if isUnknownOrEmpty(created) && isUnknownOrEmpty(skill) && len(local) == 0 && !hasRemote {
			t.Fatalf("T9: creating a skill/project did not allocate a Store repo (git.create cmd=%q payload=%v skill.create cmd=%q payload=%v)", created.Command, created.Payload, skill.Command, skill.Payload)
		}
		if !hasRemote {
			t.Fatalf("T9: new skill must have a Store remote (Hub/vsock), not only a cwd git dir (local=%v blob=%s)", local, blob)
		}
	})
}

func TestT10_RollbackOpensNewPR(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "pr.create", map[string]interface{}{
			"id":     "pr-t10",
			"repo":   "skill",
			"tenant": "tenant-a",
			"title":  "original",
		})
		resp := h.rpc("store", "pr.rollback", map[string]interface{}{
			"id":     "pr-t10",
			"repo":   "skill",
			"tenant": "tenant-a",
			"ref":    "HEAD",
		})
		if isUnknownOrEmpty(resp) {
			t.Fatalf("T10: pr.rollback missing (unknown/empty is not a handled rollback that opens a new Court-required PR); cmd=%q payload=%v", resp.Command, resp.Payload)
		}
		if resp.Command == "pr.merged" || resp.Command == "ok" || resp.Command == "git.pushed" {
			t.Fatalf("T10: skip-Court reset is not a rollback: cmd=%q payload=%v", resp.Command, resp.Payload)
		}
		newID := "pr-t10"
		if m, ok := resp.Payload.(map[string]interface{}); ok {
			for _, k := range []string{"id", "pr_id", "new_pr", "new_id", "proposal_id"} {
				if s, ok := m[k].(string); ok && s != "" {
					newID = s
					break
				}
			}
		}
		got := h.rpc("client", "pr.get", map[string]interface{}{"id": newID})
		merge := h.rpc("store", "pr.merge", map[string]interface{}{"id": newID})
		text := payloadText(got) + " " + payloadText(resp)
		needsCourt := strings.Contains(text, "court") || strings.Contains(text, "pending") || strings.Contains(text, "not approved")
		if !needsCourt && !handledDeny(merge, "court", "not approved") {
			t.Fatalf("T10: handled rollback must open a new PR that still needs Court (rollback cmd=%q payload=%v get cmd=%q payload=%v merge cmd=%q payload=%v)", resp.Command, resp.Payload, got.Command, got.Payload, merge.Command, merge.Payload)
		}
	})
}

func TestT11_DestroyedBuilderLeavesNoState(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		_ = os.WriteFile(filepath.Join(h.cwd, ".git-credentials"), []byte("https://builder:tok@example.test\n"), 0600)
		_ = os.MkdirAll(filepath.Join(h.cwd, "builder-work"), 0755)
		destroyCmds := []string{
			"builder.destroy",
			"builder.destroyed",
			"destroy.builder",
			"builder.wipe",
			"vm.destroy",
			"sandbox.destroy",
		}
		handled := false
		for _, cmd := range destroyCmds {
			resp := h.rpc("store", cmd, map[string]interface{}{
				"id":         "builder-1",
				"builder_id": "builder-1",
				"tenant":     "tenant-a",
			})
			if !isUnknownOrEmpty(resp) {
				handled = true
			}
		}
		leftoverGit := absRepos(listGitRepos(h.cwd))
		var leftoverCreds []string
		for _, name := range []string{".git-credentials", ".netrc", "id_rsa", "id_ed25519"} {
			if _, err := os.Stat(filepath.Join(h.cwd, name)); err == nil {
				leftoverCreds = append(leftoverCreds, name)
			}
		}
		if _, err := os.Stat(filepath.Join(h.cwd, "builder-work")); err == nil {
			leftoverCreds = append(leftoverCreds, "builder-work")
		}
		if !handled || len(leftoverGit) > 0 || len(leftoverCreds) > 0 {
			t.Fatalf("T11: builder destroy missing or leftover git/creds (handled=%v git=%v creds=%v)", handled, leftoverGit, leftoverCreds)
		}
	})
}

func TestT12_ForcePushHistoryDeleteFakeCourtFail(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		_ = h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		force := h.rpc("builder", "git.push", map[string]interface{}{
			"repo":    "skill",
			"tenant":  "tenant-a",
			"force":   true,
			"refspec": "+HEAD:refs/heads/main",
		})
		del := h.rpc("builder", "git.push", map[string]interface{}{
			"repo":        "skill",
			"tenant":      "tenant-a",
			"delete_refs": true,
			"refspec":     ":refs/heads/main",
		})
		if !handledDeny(force, "force", "fast-forward", "history") {
			t.Errorf("T12: force-push must be handled deny (got cmd=%q payload=%v); stub success is not a deny", force.Command, force.Payload)
		}
		if !handledDeny(del, "delete", "history", "ref") {
			t.Errorf("T12: delete-refs must be handled deny (got cmd=%q payload=%v); stub success is not a deny", del.Command, del.Payload)
		}
		_ = h.rpc("client", "proposal.create", map[string]interface{}{
			"id":          "prop-t12",
			"description": "fake court",
		})
		_ = h.rpc("court-scribe", "court.review_complete", map[string]interface{}{
			"proposal_id": "prop-t12",
			"approved":    true,
			"votes":       map[string]interface{}{"ciso": "approve"},
		})
		_ = h.rpc("builder", "pr.create", map[string]interface{}{
			"id":          "pr-t12",
			"repo":        "skill",
			"tenant":      "tenant-a",
			"proposal_id": "prop-t12",
		})
		merge := h.rpc("store", "pr.merge", map[string]interface{}{
			"id":          "pr-t12",
			"proposal_id": "prop-t12",
		})
		if !handledDeny(merge, "court", "merkle", "signature", "unsigned", "sig") {
			t.Errorf("T12: fake Court (approved without decision_merkle/sig) then pr.merge must be handled deny (got cmd=%q payload=%v)", merge.Command, merge.Payload)
		}
	})
}

func TestT13_HubVsockOnlyNoHostGitDaemonNoSkillDotGit(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		cloned := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		repos := absRepos(listGitRepos(h.cwd))
		usedLocalBare := false
		usedFileOrTCP := false
		for _, repo := range repos {
			if gitDirLooksBare(repo) {
				usedLocalBare = true
			}
			if filepath.Base(filepath.Dir(repo)) == "repos" {
				usedLocalBare = true
			}
			out, err := exec.Command("git", "-C", repo, "remote", "-v").CombinedOutput()
			text := strings.ToLower(string(out))
			if err == nil && (strings.Contains(text, "file://") || strings.Contains(text, "git://") || strings.Contains(text, "http://") || strings.Contains(text, "https://")) {
				usedFileOrTCP = true
			}
			cfg, _ := exec.Command("git", "-C", repo, "config", "--list").Output()
			ct := strings.ToLower(string(cfg))
			if strings.Contains(ct, "file://") || strings.Contains(ct, "git://") {
				usedFileOrTCP = true
			}
		}
		daemons := gitDaemonListening()
		hostGit := hostSkillDotGit(gitWorktree)
		hostGit = append(hostGit, hostSkillDotGit(h.cwd)...)
		leaks := worktreeReposLeftover()
		if usedLocalBare || usedFileOrTCP {
			t.Errorf("T13: git.clone used local git init --bare / file:// / TCP rather than Hub/vsock (bare=%v fileOrTCP=%v repos=%v)", usedLocalBare, usedFileOrTCP, repos)
		}
		if len(daemons) > 0 {
			t.Errorf("T13: host git daemon listening on %v", daemons)
		}
		if len(hostGit) > 0 {
			t.Errorf("T13: workspace/skills/.git exists: %v", hostGit)
		}
		if len(leaks) > 0 {
			t.Errorf("T13: worktree repos/ leftover from Store cwd leakage: %v", leaks)
		}
		// Pass only when clone is Hub/vsock and none of the local/tcp/daemon/host.git holes fired.
		// Do not fail the clean case — that would stay red after a correct replace.
		if !usedLocalBare && !usedFileOrTCP && len(daemons) == 0 && len(hostGit) == 0 && len(leaks) == 0 {
			if !looksLikeHubVsock(cloned.Payload) {
				t.Errorf("T13: git.clone did not return a Hub/vsock remote (cmd=%q payload=%v)", cloned.Command, cloned.Payload)
			}
		}
	})
}
