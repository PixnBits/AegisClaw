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

func payloadIsOK(v interface{}) bool {
	t := strings.TrimSpace(strings.ToLower(fmt.Sprint(v)))
	return t == "" || t == "ok" || t == "<nil>"
}

func localGitPath(v interface{}) bool {
	t := strings.TrimSpace(fmt.Sprint(v))
	low := strings.ToLower(t)
	if strings.HasPrefix(t, "/") || strings.HasPrefix(low, "file:") || strings.Contains(t, "/repos/") || strings.HasPrefix(t, "repos/") {
		return true
	}
	return false
}

func remoteFromClone(resp Message) string {
	if resp.Command == "error" || isUnknownOrEmpty(resp) {
		return ""
	}
	switch p := resp.Payload.(type) {
	case string:
		s := strings.TrimSpace(p)
		if s == "" || strings.EqualFold(s, "ok") || strings.Contains(strings.ToUpper(s), "ERR_") {
			return ""
		}
		return s
	case map[string]interface{}:
		for _, k := range []string{"remote", "url", "clone_url", "git_url", "vsock"} {
			if s, ok := p[k].(string); ok && strings.TrimSpace(s) != "" && !strings.EqualFold(s, "ok") {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func secondCloneYieldsCommit(t *testing.T, resp Message, hash string) bool {
	t.Helper()
	remote := remoteFromClone(resp)
	if remote == "" || payloadIsOK(resp.Payload) || localGitPath(remote) {
		return false
	}
	dest := filepath.Join(t.TempDir(), "t7-from-store")
	cmd := exec.Command("git", "clone", remote, dest)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("T7: git clone of Store remote %q: %s (%v)", remote, out, err)
		return false
	}
	typ, err := exec.Command("git", "-C", dest, "cat-file", "-t", hash).CombinedOutput()
	return err == nil && strings.TrimSpace(string(typ)) == "commit"
}

func makeDetachedCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=t7", "GIT_AUTHOR_EMAIL=t7@test", "GIT_COMMITTER_NAME=t7", "GIT_COMMITTER_EMAIL=t7@test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s (%v)", args, out, err)
		}
	}
	run("git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "t7.txt"), []byte("t7\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "t7.txt")
	run("git", "commit", "-q", "-m", "t7")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %s (%v)", out, err)
	}
	return strings.TrimSpace(string(out))
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
		aResp := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		bResp := h.rpc("builder", "git.clone", clonePayload("tenant-b", "skill"))
		afterA := absRepos(listGitRepos(h.cwd))
		afterB := absRepos(listGitRepos(h.cwd))
		sharedDisk := false
		for _, pa := range afterA {
			for _, pb := range afterB {
				if pa == pb {
					sharedDisk = true
				}
			}
		}
		if sharedDisk && len(afterA) > 0 {
			t.Fatalf("T3: tenants tenant-a and tenant-b share on-disk repo for skill A=%v B=%v", afterA, afterB)
		}
		ra, rb := remoteFromClone(aResp), remoteFromClone(bResp)
		if len(afterA) == 0 && len(afterB) == 0 {
			if payloadIsOK(aResp.Payload) || payloadIsOK(bResp.Payload) || ra == "" || rb == "" || ra == rb || localGitPath(ra) || localGitPath(rb) {
				t.Fatalf("T3: no tenant isolation on Store remotes (a cmd=%q payload=%v b cmd=%q payload=%v)", aResp.Command, aResp.Payload, bResp.Command, bResp.Payload)
			}
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
		hash := makeDetachedCommit(t)
		_ = h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		_ = h.rpc("builder", "git.push", map[string]interface{}{
			"repo":   "skill",
			"tenant": "tenant-a",
			"ref":    "refs/heads/main",
			"pack":   hash,
		})
		cloned2 := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
		if payloadIsOK(cloned2.Payload) {
			t.Fatal("T7: git.clone payload is ok / objects missing; stub RPC is not a round-trip")
		}
		if localGitPath(cloned2.Payload) || localGitPath(remoteFromClone(cloned2)) {
			t.Fatalf("T7: second git.clone is a local repos/ path, not Store git (payload=%v)", cloned2.Payload)
		}
		if !secondCloneYieldsCommit(t, cloned2, hash) {
			t.Fatalf("T7: second git.clone RPC did not yield commit %s (cmd=%q payload=%v)", hash, cloned2.Command, cloned2.Payload)
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
		skill := h.rpc("client", "skill.create", map[string]interface{}{
			"id":     "newskill",
			"repo":   "newskill",
			"tenant": "tenant-a",
		})
		remote := remoteFromClone(created)
		if remote == "" {
			remote = remoteFromClone(skill)
		}
		if remote == "" || localGitPath(remote) || payloadIsOK(remote) {
			t.Fatalf("T9: new skill must have a Store remote (Hub/vsock git), not a cwd path (git.create cmd=%q payload=%v skill.create cmd=%q payload=%v)", created.Command, created.Payload, skill.Command, skill.Payload)
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
			if gitDirLooksBare(repo) || filepath.Base(filepath.Dir(repo)) == "repos" {
				usedLocalBare = true
			}
			out, err := exec.Command("git", "-C", repo, "remote", "-v").CombinedOutput()
			text := strings.ToLower(string(out))
			if err == nil && (strings.Contains(text, "file://") || strings.Contains(text, "git://") || strings.Contains(text, "http://") || strings.Contains(text, "https://")) {
				usedFileOrTCP = true
			}
		}
		daemons := gitDaemonListening()
		hostGit := hostSkillDotGit(gitWorktree)
		hostGit = append(hostGit, hostSkillDotGit(h.cwd)...)
		leaks := worktreeReposLeftover()
		if usedLocalBare || usedFileOrTCP {
			t.Errorf("T13: git.clone used local git init --bare / file:// / TCP rather than Hub/vsock (bare=%v fileOrTCP=%v repos=%v)", usedLocalBare, usedFileOrTCP, repos)
			return
		}
		if len(daemons) > 0 {
			t.Errorf("T13: host git daemon listening on %v", daemons)
			return
		}
		if len(hostGit) > 0 {
			t.Errorf("T13: workspace/skills/.git exists: %v", hostGit)
			return
		}
		if len(leaks) > 0 {
			t.Errorf("T13: worktree repos/ leftover from Store cwd leakage: %v", leaks)
			return
		}
		remote := remoteFromClone(cloned)
		if payloadIsOK(cloned.Payload) || remote == "" || localGitPath(remote) {
			t.Errorf("T13: git.clone did not return a Hub/vsock remote (cmd=%q payload=%v)", cloned.Command, cloned.Payload)
		}
	})
}
