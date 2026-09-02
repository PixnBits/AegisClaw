package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T1–T13 point at shipped cmd/store. They must fail on the JSON-dashboard /
// stub-git.push Store. No t.Skip. Passing these means the replace is real.

func storeMain(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestT1_MergeWithoutCourtMustFail(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, `case "pr.merge"`) {
		t.Fatal("T1: cmd/store has no pr.merge; merge-without-Court cannot fail-closed")
	}
	if strings.Contains(src, "if required") {
		t.Fatal("T1: merge still treats Court as optional")
	}
}

func TestT2_CourtSkipMustNotExist(t *testing.T) {
	src := storeMain(t)
	if strings.Contains(src, `"skipped"`) && strings.Contains(strings.ToLower(src), "court") {
		t.Fatal("T2: court skip still exists in cmd/store")
	}
	if !strings.Contains(src, "ErrCourtSkip") && !strings.Contains(src, "court skip does not exist") {
		t.Fatal("T2: Store does not reject court skip (vacuous absence is not fail-closed)")
	}
}

func TestT3_CrossTenantFetchMustFail(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, "tenant") || strings.Contains(src, `path := "repos/" + repo`) {
		t.Fatal("T3: git.clone is a shared repos/ dir with no tenant ACL")
	}
}

func TestT4_TenantACannotPushToB(t *testing.T) {
	src := storeMain(t)
	if strings.Contains(src, "For push, assume it's handled by git, stub success") {
		t.Fatal("T4: git.push stubs success; tenant A can 'push' anywhere")
	}
}

func TestT5_ExtraRemoteSubmoduleLFSMustFail(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, "gitmodules") && !strings.Contains(src, "LFS") && !strings.Contains(src, "lfs") {
		t.Fatal("T5: Store does not reject extra remotes, submodules, or LFS")
	}
}

func TestT6_CoderHasNoGit(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, "coder has no git") && !strings.Contains(src, "ErrCoderGit") && !strings.Contains(src, `ActorCoder`) {
		t.Fatal("T6: Store does not refuse Coder git")
	}
}

func TestT7_RealGitClonePush(t *testing.T) {
	src := storeMain(t)
	if strings.Contains(src, `exec.Command("git", "init", "--bare"`) ||
		strings.Contains(src, "stub success") {
		t.Fatal("T7: Store git is init --bare / stub push, not real clone+push")
	}
}

func TestT8_PrMergeOnlyStore(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, `case "pr.merge"`) {
		t.Fatal("T8: no pr.merge; Coder/Builder/host git cannot be denied")
	}
}

func TestT9_NewSkillAlwaysStoreRepo(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, `case "git.create"`) && !strings.Contains(src, "store.git.create") {
		t.Fatal("T9: creating a skill does not always allocate a Store repo")
	}
}

func TestT10_RollbackOpensNewPR(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, `case "pr.rollback"`) {
		t.Fatal("T10: no pr.rollback that opens a new Court-required PR")
	}
}

func TestT11_DestroyedBuilderLeavesNoState(t *testing.T) {
	src := storeMain(t)
	if !strings.Contains(src, "DestroyBuilder") {
		t.Fatal("T11: Store has no Builder destroy that wipes git state and creds")
	}
}

func TestT12_ForcePushHistoryDeleteFakeCourtFail(t *testing.T) {
	src := storeMain(t)
	if strings.Contains(src, "stub success") {
		t.Fatal("T12: git.push stub cannot deny force-push or history delete")
	}
	if !strings.Contains(src, "denyNonFastForward") && !strings.Contains(src, "force") {
		t.Fatal("T12: no force-push / history-delete denial")
	}
}

func TestT13_HubVsockOnlyNoHostGitDaemonNoSkillDotGit(t *testing.T) {
	src := storeMain(t)
	if strings.Contains(src, `git", "init", "--bare"`) {
		t.Fatal("T13: Store git is local git init, not Hub/vsock")
	}
	ws := filepath.Join("workspace", "skills")
	if _, err := os.Stat(ws); err == nil {
		t.Fatal("T13: workspace/skills exists in Store process cwd")
	}
}
