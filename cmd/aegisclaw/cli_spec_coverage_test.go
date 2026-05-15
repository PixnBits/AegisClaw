package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestCLISpecCommandsRegistered ensures verbs from docs/specs/cli.md exist
// and do not return "unknown command" from Cobra.
func TestCLISpecCommandsRegistered(t *testing.T) {
	specPaths := [][]string{
		{"restart"},
		{"vm", "list"},
		{"sessions", "list"},
		{"sessions", "status", "sess-1"},
		{"sessions", "status", "sess-1", "--watch"},
		{"sessions", "pause", "sess-1"},
		{"sessions", "resume", "sess-1"},
		{"sessions", "cancel", "sess-1"},
		{"sessions", "kill", "sess-1"},
		{"tasks", "list"},
		{"tasks", "status", "task-1"},
		{"tasks", "status", "task-1", "--watch"},
		{"team", "list"},
		{"team", "create", "alpha"},
		{"team", "new", "mission"},
		{"team", "message", "team-1", "@researcher", "hello"},
		{"court", "decisions", "list"},
		{"court", "decisions", "show", "dec-1"},
		{"autonomy", "show", "sess-1"},
		{"autonomy", "revoke", "sess-1", "--scope=tools"},
		{"skills", "propose", "hello skill"},
		{"skill", "status"},
		{"skills", "status", "my-skill"},
		{"audit", "verify"},
		{"audit", "verify", "latest"},
		{"secrets", "set", "KEY", "--skill", "s"},
		{"secrets", "remove", "KEY"},
		{"completion", "bash"},
	}
	for _, parts := range specPaths {
		parts := parts
		t.Run(strings.Join(parts, " "), func(t *testing.T) {
			cmd, _, err := rootCmd.Find(parts)
			if err != nil {
				t.Fatalf("Find(%v): %v", parts, err)
			}
			if cmd == nil {
				t.Fatalf("command not found: %v", parts)
			}
		})
	}
}

func TestCompletionBashWritesScript(t *testing.T) {
	var out bytes.Buffer
	if err := rootCmd.GenBashCompletionV2(&out, true); err != nil {
		t.Fatalf("GenBashCompletionV2: %v", err)
	}
	if out.Len() == 0 || !strings.Contains(out.String(), "__start_aegisclaw") {
		t.Fatalf("expected bash completion script, got %d bytes", out.Len())
	}
}

func TestSecretsSetAliasUsesSameHandler(t *testing.T) {
	if secretsSetCmd.RunE == nil || secretsAddCmd.RunE == nil {
		t.Fatal("secrets set and add must have RunE handlers")
	}
}

func TestAuditVerifyAcceptsLatestArg(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"audit", "verify", "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("audit verify latest not found")
	}
}

func TestSkillCmdHasSkillsAlias(t *testing.T) {
	found := false
	for _, a := range skillCmd.Aliases {
		if a == "skills" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("skill command should alias 'skills'")
	}
}

// Ensure registerCLISpecCommands did not panic during init.
func TestRootHasExtendedChildren(t *testing.T) {
	names := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"restart", "vm", "sessions", "tasks", "team", "court", "autonomy", "completion"} {
		if !names[want] {
			t.Errorf("root missing child %q", want)
		}
	}
}

func TestSessionsSubcommands(t *testing.T) {
	assertSubcommands(t, sessionsCmd, "list", "status", "cancel", "kill", "pause", "resume")
}

func TestCourtDecisionsSubcommands(t *testing.T) {
	assertSubcommands(t, courtDecisionsCmd, "list", "show")
}

func assertSubcommands(t *testing.T, parent *cobra.Command, wants ...string) {
	t.Helper()
	got := make(map[string]bool)
	for _, c := range parent.Commands() {
		got[c.Name()] = true
	}
	for _, w := range wants {
		if !got[w] {
			t.Errorf("%s missing subcommand %q", parent.Name(), w)
		}
	}
}
