package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"AegisClaw/internal/storegit"
)

func initBareWithCommit(t *testing.T, tenant, repo, file, contents string) (bare, sha string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	bare, err = ensureBareRepo(tenant, repo)
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=ff",
			"GIT_AUTHOR_EMAIL=ff@test",
			"GIT_COMMITTER_NAME=ff",
			"GIT_COMMITTER_EMAIL=ff@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run(wt, "init", "-q")
	if err := os.WriteFile(filepath.Join(wt, file), []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	run(wt, "add", file)
	run(wt, "commit", "-q", "-m", file)
	out, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	sha = strings.TrimSpace(string(out))
	abs, err := filepath.Abs(bare)
	if err != nil {
		t.Fatal(err)
	}
	run(wt, "push", "-q", abs, "HEAD:refs/heads/main")
	return bare, sha
}

func TestFastForwardBareMainMovesMain(t *testing.T) {
	bare, shaA := initBareWithCommit(t, "tenant-a", "skill", "a.txt", "a\n")
	wt := t.TempDir()
	abs, err := filepath.Abs(bare)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "clone", "-q", abs, wt)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s (%v)", out, err)
	}
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = wt
		c.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=ff",
			"GIT_AUTHOR_EMAIL=ff@test",
			"GIT_COMMITTER_NAME=ff",
			"GIT_COMMITTER_EMAIL=ff@test",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run("add", "b.txt")
	run("commit", "-q", "-m", "b")
	out, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	shaB := strings.TrimSpace(string(out))
	run("push", "-q", abs, "HEAD:refs/heads/pr-ff")
	if err := fastForwardBareMain("tenant-a", "skill", shaB); err != nil {
		t.Fatalf("ff: %v", err)
	}
	got, err := exec.Command("git", "-C", bare, "rev-parse", "refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != shaB {
		t.Fatalf("main=%s want B=%s (A=%s)", strings.TrimSpace(string(got)), shaB, shaA)
	}
}

func TestFastForwardBareMainRejectsNonAncestor(t *testing.T) {
	bare, shaA := initBareWithCommit(t, "tenant-a", "skill", "a.txt", "a\n")
	wt := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=ff",
			"GIT_AUTHOR_EMAIL=ff@test",
			"GIT_COMMITTER_NAME=ff",
			"GIT_COMMITTER_EMAIL=ff@test",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	run(wt, "init", "-q")
	if err := os.WriteFile(filepath.Join(wt, "sib.txt"), []byte("sib\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(wt, "add", "sib.txt")
	run(wt, "commit", "-q", "-m", "sib")
	out, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	sib := strings.TrimSpace(string(out))
	abs, err := filepath.Abs(bare)
	if err != nil {
		t.Fatal(err)
	}
	run(wt, "push", "-q", abs, "HEAD:refs/heads/pr-sib")
	err = fastForwardBareMain("tenant-a", "skill", sib)
	if err == nil || !strings.Contains(err.Error(), "non-fast-forward") {
		t.Fatalf("want non-fast-forward, got %v", err)
	}
	got, err := exec.Command("git", "-C", bare, "rev-parse", "refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != shaA {
		t.Fatalf("main moved: %s want A=%s", strings.TrimSpace(string(got)), shaA)
	}
}

func TestBarePathTenantPrefix(t *testing.T) {
	if storegit.BarePath("tenant-a", "skill") != filepath.Join("repos", "tenant-a", "skill") {
		t.Fatal(storegit.BarePath("tenant-a", "skill"))
	}
}
