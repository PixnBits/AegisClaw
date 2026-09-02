package storegit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitEnv(dir string) []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=tester",
		"GIT_AUTHOR_EMAIL=tester@example.test",
		"GIT_COMMITTER_NAME=tester",
		"GIT_COMMITTER_EMAIL=tester@example.test",
	)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = gitEnv(dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v in %s: %s (%v)", args, dir, out, err)
	}
}

func commitFile(t *testing.T, work, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "git", "add", name)
	run(t, work, "git", "-c", "user.name=tester", "-c", "user.email=tester@example.test", "commit", "-m", "c "+name)
}

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func setupWork(t *testing.T, s *Store, tenant, repo string) string {
	t.Helper()
	if _, err := s.CreateRepo(tenant, repo); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "w")
	if err := s.Clone(ActorBuilder, tenant, repo, work); err != nil {
		t.Fatal(err)
	}
	run(t, work, "git", "checkout", "-B", "main")
	commitFile(t, work, "README", "hello\n")
	if err := s.Push(ActorBuilder, tenant, repo, work, "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	return work
}

func TestT1_MergeWithoutCourtFails(t *testing.T) {
	s := newStore(t)
	setupWork(t, s, "a", "skill")
	pr, err := s.CreatePR(ActorBuilder, "a", "skill", "pr1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge(ActorStore, pr.ID); err == nil {
		t.Fatal("expected merge without court to fail")
	}
}

func TestT2_CourtSkipMustNotExist(t *testing.T) {
	s := newStore(t)
	setupWork(t, s, "a", "skill")
	pr, _ := s.CreatePR(ActorBuilder, "a", "skill", "pr1", "main")
	if err := s.CourtReview(ActorCourt, pr.ID, "skipped"); err == nil {
		t.Fatal("court skip must not exist")
	}
	if err := s.CourtReview(ActorCourt, pr.ID, "skip"); err == nil {
		t.Fatal("court skip must not exist")
	}
}

func TestT3_CrossTenantFetchFails(t *testing.T) {
	s := newStore(t)
	setupWork(t, s, "a", "skill")
	if _, err := s.CreateRepo("b", "other"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "steal")
	if err := s.Clone(ActorBuilder, "b", "skill", dest); err == nil {
		t.Fatal("tenant b must not fetch tenant a repo by name")
	}
}

func TestT4_TenantACannotPushToB(t *testing.T) {
	s := newStore(t)
	work := setupWork(t, s, "a", "skill")
	if _, err := s.CreateRepo("b", "skill"); err != nil {
		t.Fatal(err)
	}
	if err := s.PushToTenant(ActorBuilder, "a", "b", "skill", work, "HEAD:refs/heads/main"); err == nil {
		t.Fatal("tenant a must not push to tenant b")
	}
}

func TestT5_ExtraRemoteSubmoduleLFSFails(t *testing.T) {
	s := newStore(t)
	work := setupWork(t, s, "a", "skill")
	if err := s.AddRemote(ActorBuilder, work, "evil", "https://evil.example/x.git"); err == nil {
		t.Fatal("extra remote must fail")
	}
	if err := s.EnableSubmoduleOrLFS(work); err == nil {
		t.Fatal("submodule/LFS must fail")
	}
}

func TestT6_CoderHasNoGit(t *testing.T) {
	s := newStore(t)
	if s.HasCoderGit() {
		t.Fatal("coder must not have git")
	}
	if _, err := s.CreateRepo("a", "skill"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clone(ActorCoder, "a", "skill", filepath.Join(t.TempDir(), "c")); err == nil {
		t.Fatal("coder clone must fail")
	}
}

func TestT7_GitClonePushAgainstOwnRemote(t *testing.T) {
	s := newStore(t)
	work := setupWork(t, s, "a", "skill")
	commitFile(t, work, "two", "2\n")
	if err := s.Push(ActorBuilder, "a", "skill", work, "HEAD:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "clone2")
	if err := s.Clone(ActorUser, "a", "skill", dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "two")); err != nil {
		t.Fatal("real git clone missing pushed file")
	}
}

func TestT8_PrMergeFromCoderBuilderHostGitFails(t *testing.T) {
	s := newStore(t)
	setupWork(t, s, "a", "skill")
	pr, _ := s.CreatePR(ActorBuilder, "a", "skill", "pr1", "main")
	if err := s.CourtReview(ActorCourt, pr.ID, CourtApproved); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{ActorCoder, ActorBuilder, ActorHostGit} {
		if err := s.Merge(actor, pr.ID); err == nil {
			t.Fatalf("%s must not merge", actor)
		}
	}
	if err := s.Merge(ActorStore, pr.ID); err != nil {
		t.Fatal(err)
	}
}

func TestT9_NewSkillAlwaysGetsStoreRepo(t *testing.T) {
	s := newStore(t)
	p, err := s.CreateRepo("a", "newskill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "HEAD")); err != nil {
		t.Fatal("expected bare repo HEAD")
	}
}

func TestT10_RollbackOpensNewPRNeedingCourt(t *testing.T) {
	s := newStore(t)
	setupWork(t, s, "a", "skill")
	pr, err := s.Rollback(ActorBuilder, "a", "skill", "rb1", "HEAD~0")
	if err != nil {
		t.Fatal(err)
	}
	if pr.Court != CourtPending {
		t.Fatalf("rollback PR court=%s", pr.Court)
	}
	if err := s.Merge(ActorStore, pr.ID); err == nil {
		t.Fatal("rollback must not merge without court")
	}
}

func TestT11_DestroyedBuilderLeavesNoGitStateOrCreds(t *testing.T) {
	s := newStore(t)
	sess, err := s.StartBuilder("a")
	if err != nil {
		t.Fatal(err)
	}
	if !s.BuilderExists(sess.id) {
		t.Fatal("expected session")
	}
	if err := s.DestroyBuilder(sess.id); err != nil {
		t.Fatal(err)
	}
	if s.BuilderExists(sess.id) {
		t.Fatal("builder session must be gone")
	}
	if _, err := os.Stat(sess.worktree); !os.IsNotExist(err) {
		t.Fatal("worktree leftover")
	}
	if _, err := os.Stat(sess.creds); !os.IsNotExist(err) {
		t.Fatal("creds leftover")
	}
}

func TestT12_ForcePushHistoryDeleteFakeCourtFail(t *testing.T) {
	s := newStore(t)
	work := setupWork(t, s, "a", "skill")
	if err := s.Push(ActorBuilder, "a", "skill", work, "+HEAD:refs/heads/main"); err == nil {
		t.Fatal("force-push refspec must fail")
	}
	// rewrite history then force
	run(t, work, "git", "-c", "user.name=tester", "-c", "user.email=tester@example.test", "commit", "--amend", "-m", "rewrite")
	if err := s.Push(ActorBuilder, "a", "skill", work, "HEAD:refs/heads/main"); err == nil {
		t.Fatal("history rewrite push must fail")
	}
	pr, _ := s.CreatePR(ActorBuilder, "a", "skill", "pr1", "main")
	if err := s.CourtReview(ActorBuilder, pr.ID, CourtApproved); err == nil {
		t.Fatal("fake court from builder must fail")
	}
	if err := s.CourtReview(ActorCoder, pr.ID, CourtApproved); err == nil {
		t.Fatal("fake court from coder must fail")
	}
}

func TestT13_NoGitDaemonNoHostSkillGitHubVsockOnly(t *testing.T) {
	s := newStore(t)
	if s.GitDaemonBound() {
		t.Fatal("must not bind git daemon")
	}
	if err := s.ListenGitDaemon("127.0.0.1:9418"); err == nil {
		t.Fatal("listen git daemon must fail")
	}
	if err := s.ProbeGitDaemon("127.0.0.1:9418"); err == nil {
		t.Fatal("nothing should accept git:// on 9418")
	}
	if err := s.WriteHostSkillGit("SecurityAuditor"); err == nil {
		t.Fatal("workspace/skills/.git must be forbidden")
	}
	ws := t.TempDir()
	if s.HostSkillGitExists(ws, "SecurityAuditor") {
		t.Fatal("must not create host per-skill .git")
	}
}
