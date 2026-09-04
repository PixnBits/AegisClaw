package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"AegisClaw/internal/storegit"
)

var gitSHARe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

func looksLikeGitSHA(s string) bool {
	return gitSHARe.MatchString(strings.TrimSpace(s))
}

func gitBare(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// fastForwardBareMain moves refs/heads/main to sha only if sha is a commit in
// the tenant bare repo and is a fast-forward of current main (or main is absent).
func fastForwardBareMain(tenant, repo, sha string) error {
	tenant = strings.TrimSpace(tenant)
	repo = strings.TrimSpace(repo)
	sha = strings.TrimSpace(sha)
	if !storegit.ValidName(tenant) || !storegit.ValidName(repo) {
		return fmt.Errorf("invalid tenant or repo")
	}
	if !looksLikeGitSHA(sha) {
		return fmt.Errorf("missing merge sha")
	}
	bare := storegit.BarePath(tenant, repo)
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		return fmt.Errorf("repo missing")
	}
	if out, err := gitBare(bare, "cat-file", "-e", sha+"^{commit}").CombinedOutput(); err != nil {
		return fmt.Errorf("unknown commit: %s", strings.TrimSpace(string(out)))
	}
	cur, err := gitBare(bare, "rev-parse", "-q", "--verify", "refs/heads/main").CombinedOutput()
	if err == nil {
		old := strings.TrimSpace(string(cur))
		if old != "" && !strings.EqualFold(old, sha) {
			if err := gitBare(bare, "merge-base", "--is-ancestor", old, sha).Run(); err != nil {
				return fmt.Errorf("non-fast-forward")
			}
		}
	}
	if out, err := gitBare(bare, "update-ref", "refs/heads/main", sha).CombinedOutput(); err != nil {
		return fmt.Errorf("update-ref: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
