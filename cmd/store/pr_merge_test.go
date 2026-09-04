package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"AegisClaw/internal/storegit"
)

func tenantBare(h *liveHub, tenant, repo string) string {
	return filepath.Join(h.cwd, storegit.BarePath(tenant, repo))
}

func revParseRef(t *testing.T, bare, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", bare, "rev-parse", ref).CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s rev-parse %s: %s (%v)", bare, ref, out, err)
	}
	return strings.TrimSpace(string(out))
}

func gitCommitFile(t *testing.T, dir, name, contents, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=pr-merge",
			"GIT_AUTHOR_EMAIL=pr-merge@test",
			"GIT_COMMITTER_NAME=pr-merge",
			"GIT_COMMITTER_EMAIL=pr-merge@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run("add", name)
	run("commit", "-q", "-m", msg)
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %s (%v)", out, err)
	}
	return strings.TrimSpace(string(out))
}

func pushRef(t *testing.T, h *liveHub, tenant, dir, remote, refspec string) {
	t.Helper()
	cmd := exec.Command("git", "push", remote, refspec)
	cmd.Dir = dir
	if out, err := h.gitAs(tenant, cmd); err != nil {
		t.Fatalf("git push %s %s: %s (%v)", remote, refspec, out, err)
	}
}

func courtApproveSigned(t *testing.T, h *liveHub, proposalID string) {
	t.Helper()
	created := h.rpc("client", "proposal.create", map[string]interface{}{
		"id":          proposalID,
		"description": "court-approved merge",
	})
	if created.Command != "proposal.created" {
		t.Fatalf("proposal.create: cmd=%q payload=%v", created.Command, created.Payload)
	}
	got := h.rpc("court-scribe", "court.review_complete", map[string]interface{}{
		"proposal_id":     proposalID,
		"approved":        true,
		"decision_merkle": "test-merkle",
		"decision_sig":    "test-sig",
		"votes":           map[string]interface{}{"ciso": "approve", "lawyer": "approve"},
	})
	if got.Command == "error" || isUnknownOrEmpty(got) {
		t.Fatalf("court.review_complete: cmd=%q payload=%v", got.Command, got.Payload)
	}
}

func setupTenantSkillRemote(t *testing.T, h *liveHub) string {
	t.Helper()
	cloned := h.rpc("builder", "git.clone", clonePayload("tenant-a", "skill"))
	remote := remoteFromClone(cloned)
	if remote == "" || payloadIsOK(cloned.Payload) || localGitPath(remote) {
		t.Fatalf("git.clone did not return a Store remote (cmd=%q payload=%v)", cloned.Command, cloned.Payload)
	}
	return remote
}

func jsonMergedTrue(resp Message) bool {
	m, ok := resp.Payload.(map[string]interface{})
	if !ok {
		return false
	}
	v, ok := m["merged"].(bool)
	return ok && v
}

func TestPRMerge_FastForwardMovesMain(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		remote := setupTenantSkillRemote(t, h)
		dir, hashA := makeDetachedCommit(t)
		pushRef(t, h, "tenant-a", dir, remote, "HEAD:refs/heads/main")
		hashB := gitCommitFile(t, dir, "b.txt", "b\n", "b")
		if hashB == hashA {
			t.Fatalf("expected distinct child commit, A=B=%s", hashA)
		}
		pushRef(t, h, "tenant-a", dir, remote, "HEAD:refs/heads/pr-ff")

		bare := tenantBare(h, "tenant-a", "skill")
		if got := revParseRef(t, bare, "refs/heads/main"); got != hashA {
			t.Fatalf("before merge, main=%s want A=%s (objects of B must not move main)", got, hashA)
		}

		courtApproveSigned(t, h, "prop-ff")
		_ = h.rpc("builder", "pr.create", map[string]interface{}{
			"id":          "pr-ff",
			"repo":        "skill",
			"tenant":      "tenant-a",
			"proposal_id": "prop-ff",
			"sha":         hashB,
		})
		resp := h.rpc("store", "pr.merge", map[string]interface{}{
			"id":          "pr-ff",
			"proposal_id": "prop-ff",
			"tenant":      "tenant-a",
			"repo":        "skill",
			"sha":         hashB,
		})
		if resp.Command != "pr.merged" {
			t.Fatalf("pr.merge as store must return pr.merged (got cmd=%q payload=%v)", resp.Command, resp.Payload)
		}
		got := revParseRef(t, bare, "refs/heads/main")
		if got != hashB {
			t.Fatalf("refs/heads/main must move to B=%s (got %s, A=%s); JSON-only merged is a miss", hashB, got, hashA)
		}
		if got == hashA {
			t.Fatalf("main still at A after pr.merged")
		}
	})
}

func TestCourtUnsignedApprovedDoesNotMarkApproved(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		created := h.rpc("client", "proposal.create", map[string]interface{}{
			"id":          "prop-unsigned",
			"description": "approved bool without merkle",
		})
		if created.Command != "proposal.created" {
			t.Fatalf("proposal.create: cmd=%q payload=%v", created.Command, created.Payload)
		}
		_ = h.rpc("court-scribe", "court.review_complete", map[string]interface{}{
			"proposal_id": "prop-unsigned",
			"approved":    true,
			"votes":       map[string]interface{}{"ciso": "approve"},
		})
		got := h.rpc("client", "proposal.get", map[string]interface{}{"id": "prop-unsigned"})
		if t2MarkedApprovedOrMergeable(got) {
			t.Fatalf("unsigned approved:true must not mark proposal approved (payload=%v)", got.Payload)
		}
	})
}

func TestPRMerge_NonFastForwardDenied(t *testing.T) {
	withLiveStore(t, func(h *liveHub) {
		remote := setupTenantSkillRemote(t, h)
		dirA, hashA := makeDetachedCommit(t)
		pushRef(t, h, "tenant-a", dirA, remote, "HEAD:refs/heads/main")
		dirSib := t.TempDir()
		runInit := exec.Command("git", "init", "-q")
		runInit.Dir = dirSib
		runInit.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := runInit.CombinedOutput(); err != nil {
			t.Fatalf("git init sib: %s (%v)", out, err)
		}
		hashSib := gitCommitFile(t, dirSib, "sib.txt", "unrelated sibling\n", "sib")
		if hashSib == hashA {
			t.Fatalf("sibling SHA collided with A")
		}
		pushRef(t, h, "tenant-a", dirSib, remote, "HEAD:refs/heads/pr-sib")

		bare := tenantBare(h, "tenant-a", "skill")
		if got := revParseRef(t, bare, "refs/heads/main"); got != hashA {
			t.Fatalf("before merge, main=%s want A=%s", got, hashA)
		}

		courtApproveSigned(t, h, "prop-nff")
		_ = h.rpc("builder", "pr.create", map[string]interface{}{
			"id":          "pr-nff",
			"repo":        "skill",
			"tenant":      "tenant-a",
			"proposal_id": "prop-nff",
			"sha":         hashSib,
		})
		resp := h.rpc("store", "pr.merge", map[string]interface{}{
			"id":          "pr-nff",
			"proposal_id": "prop-nff",
			"tenant":      "tenant-a",
			"repo":        "skill",
			"sha":         hashSib,
		})
		if resp.Command == "pr.merged" {
			t.Fatalf("sibling (non-ancestor) merge must deny, not pr.merged (payload=%v)", resp.Payload)
		}
		got := revParseRef(t, bare, "refs/heads/main")
		if got != hashA {
			t.Fatalf("main must stay A=%s after non-ff deny (got %s)", hashA, got)
		}
		gotPR := h.rpc("client", "pr.get", map[string]interface{}{"id": "pr-nff"})
		if jsonMergedTrue(gotPR) {
			t.Fatalf("pr.merged JSON flag must stay unset after non-ff deny (payload=%v)", gotPR.Payload)
		}
	})
}
