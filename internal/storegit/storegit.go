package storegit

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Actors that may talk to Store git/PR APIs.
const (
	ActorStore   = "store"
	ActorBuilder = "builder"
	ActorCoder   = "coder"
	ActorCourt   = "court"
	ActorHostGit = "host-git"
	ActorUser    = "user"
)

// Court states. There is no skipped.
const (
	CourtPending    = "pending"
	CourtInProgress = "in_progress"
	CourtApproved   = "approved"
	CourtRejected   = "rejected"
)

var (
	ErrDenied          = errors.New("storegit: denied")
	ErrCourtRequired   = errors.New("storegit: court approve required")
	ErrCourtSkip       = errors.New("storegit: court skip does not exist")
	ErrCrossTenant     = errors.New("storegit: cross-tenant denied")
	ErrOnlyStoreMerges = errors.New("storegit: only store may merge")
	ErrForcePush       = errors.New("storegit: force-push and history rewrite denied")
	ErrFakeCourt       = errors.New("storegit: fake court denied")
	ErrExtraRemote     = errors.New("storegit: extra remote, submodule, hook, or LFS denied")
	ErrCoderGit        = errors.New("storegit: coder has no git")
	ErrNoGitDaemon     = errors.New("storegit: host git daemon is forbidden")
	ErrHostSkillGit    = errors.New("storegit: workspace/skills/.git is forbidden")
)

// Store is the tenant-scoped git + PR authority. It never starts a git daemon.
type Store struct {
	root     string
	mu       sync.Mutex
	tenants  map[string]*tenant
	prs      map[string]*PR
	builders map[string]*builderSession
}

type tenant struct {
	id    string
	repos map[string]string // repo name -> bare git dir
}

type builderSession struct {
	id       string
	tenant   string
	worktree string
	creds    string
}

// PR is Store PR state. Court skip is not a field.
type PR struct {
	ID         string
	Tenant     string
	Repo       string
	FromRef    string
	Court      string
	CourtActor string
	Merged     bool
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{
		root:     root,
		tenants:  map[string]*tenant{},
		prs:      map[string]*PR{},
		builders: map[string]*builderSession{},
	}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) tenantLocked(id string) *tenant {
	t, ok := s.tenants[id]
	if !ok {
		t = &tenant{id: id, repos: map[string]string{}}
		s.tenants[id] = t
	}
	return t
}

// CreateRepo always allocates a Store bare repo for a new skill or project (T9).
func (s *Store) CreateRepo(tenantID, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createRepoLocked(tenantID, name)
}

func (s *Store) createRepoLocked(tenantID, name string) (string, error) {
	if strings.Contains(name, "..") || name == "" {
		return "", ErrDenied
	}
	t := s.tenantLocked(tenantID)
	if p, ok := t.repos[name]; ok {
		return p, nil
	}
	bare := filepath.Join(s.root, "tenants", tenantID, "repos", name+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o700); err != nil {
		return "", err
	}
	if err := runGit(s.root, "init", "--bare", bare); err != nil {
		return "", err
	}
	// Fail-closed receive: no force, no deletes, our hook only.
	_ = runGit(bare, "symbolic-ref", "HEAD", "refs/heads/main")
	_ = runGit(bare, "config", "receive.denyNonFastForwards", "true")
	_ = runGit(bare, "config", "receive.denyDeletes", "true")
	hook := filepath.Join(bare, "hooks", "pre-receive")
	body := `#!/bin/sh
zero=0000000000000000000000000000000000000000
while read old new ref; do
  if [ "$new" = "$zero" ]; then echo deny-delete >&2; exit 1; fi
  if [ "$old" = "$zero" ]; then
    names=$(git ls-tree -r --name-only "$new")
  else
    git merge-base --is-ancestor "$old" "$new" || { echo deny-non-ff >&2; exit 1; }
    names=$(git diff-tree -r --name-only "$old" "$new")
  fi
  echo "$names" | grep -E '(^|/)\.gitmodules$' >/dev/null && { echo deny-submodule >&2; exit 1; } || true
  echo "$names" | grep -E '(^|/)\.lfsconfig$' >/dev/null && { echo deny-lfs >&2; exit 1; } || true
done
exit 0
`
	if err := os.WriteFile(hook, []byte(body), 0o700); err != nil {
		return "", err
	}
	t.repos[name] = bare
	return bare, nil
}

func (s *Store) bare(tenantID, name string) (string, error) {
	t, ok := s.tenants[tenantID]
	if !ok {
		return "", ErrCrossTenant
	}
	p, ok := t.repos[name]
	if !ok {
		return "", ErrCrossTenant
	}
	return p, nil
}

// Clone is real git clone of that tenant's Store remote into a worktree.
func (s *Store) Clone(actor, tenantID, repo, dest string) error {
	if actor == ActorCoder {
		return ErrCoderGit
	}
	s.mu.Lock()
	bare, err := s.bare(tenantID, repo)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if dest == "" {
		return ErrDenied
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return runGit(s.root, "clone", bare, dest)
}

// Push is real git push of dest's HEAD to the tenant's Store remote.
func (s *Store) Push(actor, tenantID, repo, worktree, refspec string) error {
	if actor == ActorCoder {
		return ErrCoderGit
	}
	s.mu.Lock()
	bare, err := s.bare(tenantID, repo)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if refspec == "" {
		refspec = "HEAD:refs/heads/main"
	}
	if strings.HasPrefix(refspec, "+") {
		return ErrForcePush
	}
	cmd := exec.Command("git", "-C", worktree, "push", bare, refspec)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		low := strings.ToLower(string(out) + err.Error())
		if strings.Contains(low, "non-fast-forward") || strings.Contains(low, "deny-non-ff") ||
			strings.Contains(low, "denied") || strings.Contains(low, "force") {
			return fmt.Errorf("%w: %s", ErrForcePush, bytes.TrimSpace(out))
		}
		if strings.Contains(low, "submodule") || strings.Contains(low, "lfs") {
			return fmt.Errorf("%w: %s", ErrExtraRemote, bytes.TrimSpace(out))
		}
		return fmt.Errorf("git push: %s: %w", bytes.TrimSpace(out), err)
	}
	return nil
}

func (s *Store) PushToTenant(actor, fromTenant, toTenant, repo, worktree, refspec string) error {
	if fromTenant != toTenant {
		return ErrCrossTenant
	}
	return s.Push(actor, toTenant, repo, worktree, refspec)
}

func (s *Store) AddRemote(actor, worktree, name, url string) error {
	if name != "origin" {
		return ErrExtraRemote
	}
	return runGit(worktree, "remote", "add", name, url)
}

func (s *Store) EnableSubmoduleOrLFS(_ string) error { return ErrExtraRemote }

func (s *Store) CreatePR(actor, tenantID, repo, id, fromRef string) (*PR, error) {
	if actor == ActorCoder {
		return nil, ErrCoderGit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.bare(tenantID, repo); err != nil {
		return nil, err
	}
	pr := &PR{ID: id, Tenant: tenantID, Repo: repo, FromRef: fromRef, Court: CourtPending}
	s.prs[id] = pr
	return pr, nil
}

func (s *Store) CourtReview(actor, prID, state string) error {
	if state == "skipped" || state == "skip" {
		return ErrCourtSkip
	}
	if actor != ActorCourt {
		return ErrFakeCourt
	}
	switch state {
	case CourtPending, CourtInProgress, CourtApproved, CourtRejected:
	default:
		return ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.prs[prID]
	if !ok {
		return ErrDenied
	}
	pr.Court = state
	pr.CourtActor = actor
	return nil
}

func (s *Store) Merge(actor, prID string) error {
	if actor != ActorStore {
		return ErrOnlyStoreMerges
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.prs[prID]
	if !ok {
		return ErrDenied
	}
	if pr.Court != CourtApproved || pr.CourtActor != ActorCourt {
		return ErrCourtRequired
	}
	pr.Merged = true
	return nil
}

func (s *Store) Rollback(actor, tenantID, repo, id, priorRef string) (*PR, error) {
	pr, err := s.CreatePR(actor, tenantID, repo, id, priorRef)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (s *Store) StartBuilder(tenantID string) (*builderSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := "b-" + hex.EncodeToString(b)
	wt := filepath.Join(s.root, "builders", id)
	cred := filepath.Join(s.root, "builders", id+".cred")
	if err := os.MkdirAll(wt, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cred, []byte("builder-session"), 0o600); err != nil {
		return nil, err
	}
	sess := &builderSession{id: id, tenant: tenantID, worktree: wt, creds: cred}
	s.builders[id] = sess
	return sess, nil
}

func (s *Store) DestroyBuilder(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.builders[id]
	if !ok {
		return nil
	}
	_ = os.RemoveAll(sess.worktree)
	_ = os.Remove(sess.creds)
	delete(s.builders, id)
	return nil
}

func (s *Store) BuilderExists(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.builders[id]
	return ok
}

func (s *Store) ListenGitDaemon(addr string) error {
	return ErrNoGitDaemon
}

func (s *Store) GitDaemonBound() bool { return false }

func (s *Store) ProbeGitDaemon(addr string) error {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func (s *Store) WriteHostSkillGit(skill string) error {
	return ErrHostSkillGit
}

func (s *Store) HostSkillGitExists(workspace, skill string) bool {
	_, err := os.Stat(filepath.Join(workspace, "skills", skill, ".git"))
	return err == nil
}

func (s *Store) HasCoderGit() bool { return false }

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return nil
}
