package main

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"go.uber.org/zap"
)

// TestGenerateGoMain_CompilesWithoutErrors is a regression test for the bug
// where generateGoMain emitted "fmt" in the import block but never used it,
// causing every generated skill to fail `go build` with "imported and not used".
func TestGenerateGoMain_CompilesWithoutErrors(t *testing.T) {
	cg := &InVMCodeGenerator{}
	spec := &builder.SkillSpec{
		Name:        "test_skill",
		Description: "A test skill",
		Language:    "go",
	}

	code := cg.generateGoMain(spec)

	// Sanity-check: the code must at least parse.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "main.go", code, 0); err != nil {
		t.Fatalf("generated code failed to parse: %v\ncode:\n%s", err, code)
	}

	// Write to a temp module and confirm `go build` succeeds.
	dir := t.TempDir()
	goMod := "module github.com/aegisclaw/skills/test_skill\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "build", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated code does not compile: %v\noutput:\n%s\ncode:\n%s", err, out, code)
	}
}

// TestGenerateSkillCode_RelativePathsUnderSkillSubdir verifies that the file
// map returned by generateSkillCode uses workspace-relative paths (not absolute)
// and that every path starts with "skills/<name>/".
//
// Regression: previously the map was keyed by absolute skillDir-relative paths,
// so committed files landed at incorrect locations in the git tree.
func TestGenerateSkillCode_RelativePathsUnderSkillSubdir(t *testing.T) {
	cg := &InVMCodeGenerator{}
	spec := &builder.SkillSpec{
		Name:        "my_skill",
		Description: "My skill description",
		Language:    "go",
	}

	workspaceDir := t.TempDir()
	skillDir := filepath.Join(workspaceDir, "skills", spec.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	files, reasoning, err := cg.generateSkillCode(context.Background(), spec, workspaceDir, skillDir)
	if err != nil {
		t.Fatalf("generateSkillCode: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected at least one file to be generated")
	}
	if reasoning == "" {
		t.Fatal("expected non-empty reasoning")
	}

	wantPrefix := filepath.Join("skills", spec.Name) + string(os.PathSeparator)
	for path := range files {
		if filepath.IsAbs(path) {
			t.Errorf("file path %q is absolute; want relative", path)
		}
		if !strings.HasPrefix(path, wantPrefix) {
			t.Errorf("file path %q does not start with %q", path, wantPrefix)
		}
	}
}

// TestInVMAnalyzer_Analyze_PassesWithValidGoModule verifies that Analyze
// succeeds when given a directory that contains a valid Go module (go.mod +
// compilable source).  This documents the required contract: the caller must
// pass the skill's own module directory, NOT the workspace root.
func TestInVMAnalyzer_Analyze_PassesWithValidGoModule(t *testing.T) {
	dir := t.TempDir()

	goMod := "module github.com/aegisclaw/skills/test_skill\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	mainGo := "package main\n\nimport (\n\t\"encoding/json\"\n\t\"os\"\n)\n\nfunc main() {\n\tjson.NewEncoder(os.Stdout).Encode(map[string]string{\"status\": \"ok\"})\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	testGo := "package main\n\nimport \"testing\"\n\nfunc TestSkill(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(testGo), 0644); err != nil {
		t.Fatalf("write main_test.go: %v", err)
	}

	analyzer := &InVMAnalyzer{logger: zap.NewNop()}
	result, err := analyzer.Analyze(context.Background(), dir, "go")
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}

	if !result.Passed {
		t.Errorf("expected Passed=true, got FailureReason=%q", result.FailureReason)
	}
	if !result.TestPassed {
		t.Error("expected TestPassed=true")
	}
	if !result.BuildPassed {
		t.Error("expected BuildPassed=true")
	}
}

// TestInVMAnalyzer_Analyze_FailsAtWorkspaceRoot is the canonical regression
// test for the bug where Analyze was called with the workspace root ("/workspace")
// instead of the skill module directory ("/workspace/skills/<name>").
//
// When called from a directory that has no go.mod, `go test ./...` exits with:
//
//	"pattern ./...: directory prefix . does not contain main module or its
//	selected dependencies"
//
// The fix in invm_pipeline.go Execute() ensures skillDir is passed to Analyze.
func TestInVMAnalyzer_Analyze_FailsAtWorkspaceRoot(t *testing.T) {
	workspaceDir := t.TempDir()

	// Put the go.mod ONLY inside the skill subdirectory, not the workspace root.
	skillDir := filepath.Join(workspaceDir, "skills", "some_skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	goMod := "module github.com/aegisclaw/skills/some_skill\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(skillDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	mainGo := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(skillDir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	analyzer := &InVMAnalyzer{logger: zap.NewNop()}
	// Calling with workspaceDir (no go.mod there) must NOT succeed.
	result, _ := analyzer.Analyze(context.Background(), workspaceDir, "go")
	if result != nil && result.Passed {
		t.Error("Analyze should fail when called with a directory that has no go.mod; " +
			"this indicates the skillDir regression has been reintroduced")
	}
}
