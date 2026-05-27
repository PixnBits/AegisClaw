// index_test.go — unit tests for the 7.3 local AgentSkillIndex (Jaccard/Levenshtein/search/Format).
//
// These tests provide direct coverage on the fast local semantic index that is
// injected into every step of the 6-step Agent Runtime loop.
//
// SPEC REFERENCES:
//   - agent-runtime.md §Responsibilities (7.3 local tool awareness: every reasoning step
//     receives FormatAvailableTools output; tools only from the current local index)
//   - docs/prd/security-model.md (least-privilege: the index constrains what the agent
//     may even consider invoking in any turn; fail-closed on nil/unknown)
//   - memory-vm.md + runtime-architecture.md (index is the in-VM view refreshed via Hub)
//   - no-stubs-plan/phase-1.md 1.4 (unit tests for skills package to reach aggregate
//     coverage target on internal/agent/)

package skills

import (
	"strings"
	"testing"
)

func TestNewAgentSkillIndex_SeedsKnownTools(t *testing.T) {
	idx := NewAgentSkillIndex()
	if idx == nil {
		t.Fatal("NewAgentSkillIndex returned nil")
	}
	skills := idx.ListSkills()
	if len(skills) < 2 {
		t.Errorf("expected at least 2 seeded skills, got %d", len(skills))
	}
	tools := idx.tools // access via Handle or by exercising search
	if len(tools) < 4 {
		t.Errorf("expected at least 4 seeded tools, got %d", len(tools))
	}

	// Verify known seeded entries used by step tests and Format
	foundDiscord := false
	for _, s := range skills {
		if s.ID == "discord_monitor" {
			foundDiscord = true
			break
		}
	}
	if !foundDiscord {
		t.Error("seeded discord_monitor skill not present")
	}
}

func TestListSkills_AndAdd(t *testing.T) {
	idx := NewAgentSkillIndex()
	before := len(idx.ListSkills())

	idx.AddSkill(Skill{ID: "test_skill", Name: "test_skill", Description: "for testing"})
	after := len(idx.ListSkills())
	if after != before+1 {
		t.Errorf("AddSkill did not increase count: %d -> %d", before, after)
	}

	// ListSkills must return a copy (mutation of returned slice must not affect index)
	list := idx.ListSkills()
	list[0].Name = "MUTATED"
	again := idx.ListSkills()
	if again[0].Name == "MUTATED" {
		t.Error("ListSkills did not return a defensive copy")
	}
}

func TestSearchTools_MatchesAndScoring(t *testing.T) {
	idx := NewAgentSkillIndex()

	// High Jaccard + name match on a seeded tool
	results := idx.SearchTools("discord monitor send", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'discord monitor send'")
	}
	if results[0].Score < 0.5 {
		t.Errorf("expected strong score for direct overlap, got %f", results[0].Score)
	}
	if !strings.Contains(results[0].Tool.Name, "discord_monitor") {
		t.Error("top result should be a discord_monitor tool")
	}

	// Substring bonus path + TF boost
	results = idx.SearchTools("research", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'research'")
	}

	// Short query Levenshtein boost path (len(qTokens) <= 3)
	results = idx.SearchTools("web", 5)
	if len(results) == 0 {
		t.Fatal("expected results for short 'web' query")
	}

	// Partial / lower scoring but still above filter
	results = idx.SearchTools("summarize url", 5)
	if len(results) == 0 {
		t.Error("expected results for 'summarize url'")
	}
}

func TestSearchTools_EdgeCases(t *testing.T) {
	idx := NewAgentSkillIndex()

	// Empty query -> nil (per impl)
	if res := idx.SearchTools("", 10); res != nil {
		t.Error("empty query should return nil")
	}
	if res := idx.SearchTools("   ", 10); res != nil {
		t.Error("whitespace-only query should return nil")
	}

	// Very weak match filtered (< 0.05 after all boosts)
	res := idx.SearchTools("xyzzyqwerty", 5)
	if len(res) != 0 {
		t.Errorf("very weak query should be filtered, got %d results", len(res))
	}

	// Limit respected
	res = idx.SearchTools("discord", 1)
	if len(res) > 1 {
		t.Error("limit=1 not respected")
	}
}

func TestFormatAvailableTools_NilAndWorkspace(t *testing.T) {
	// Nil index branch
	s := FormatAvailableTools(nil, nil)
	if !strings.Contains(s, "no local tool index") {
		t.Error("nil idx did not produce fallback message")
	}

	idx := NewAgentSkillIndex()

	// Normal (nil workspace) — exercises the skills + tools loops
	s = FormatAvailableTools(idx, nil)
	if !strings.Contains(s, "discord_monitor") || !strings.Contains(s, "web_research") {
		t.Error("Format did not include seeded skill names")
	}
	if !strings.Contains(s, "discord_monitor.send_message") {
		t.Error("Format did not include seeded tool names")
	}

	// Workspace injection branch (7.4/7.6 custom guidance)
	// Use the package-level testWorkspace (defined later in this file; Go package scope is fine).
	w := &testWorkspace{tools: "Always prefer audited tools.", sk: "Court review for new skills."}
	s = FormatAvailableTools(idx, w)
	if !strings.Contains(s, "Custom tool guidance: Always prefer audited tools.") {
		t.Error("workspace GetTOOLS injection failed")
	}
	if !strings.Contains(s, "Additional skills context: Court review for new skills.") {
		t.Error("workspace GetSKILLS injection failed")
	}
}

// Minimal workspace stub for the interface in FormatAvailableTools.
type testWorkspace struct {
	soul, agents, tools, sk string
}

func (w *testWorkspace) GetSOUL() string   { return w.soul }
func (w *testWorkspace) GetAGENTS() string { return w.agents }
func (w *testWorkspace) GetTOOLS() string  { return w.tools }
func (w *testWorkspace) GetSKILLS() string { return w.sk }

func TestHandleToolCommand(t *testing.T) {
	idx := NewAgentSkillIndex()

	// tool.list
	out := HandleToolCommand("tool.list", nil, idx)
	m, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("tool.list did not return map, got %T", out)
	}
	if _, has := m["skills"]; !has {
		t.Error("tool.list missing skills")
	}
	if _, has := m["tools"]; !has {
		t.Error("tool.list missing tools")
	}

	// tool.search
	payload := map[string]interface{}{"query": "discord"}
	out = HandleToolCommand("tool.search", payload, idx)
	m, ok = out.(map[string]interface{})
	if !ok {
		t.Fatalf("tool.search did not return map, got %T", out)
	}
	if m["query"] != "discord" {
		t.Error("tool.search did not echo query")
	}
	results, _ := m["results"].([]SearchResult)
	if len(results) == 0 {
		t.Error("tool.search produced no results for valid query")
	}

	// unknown command
	out = HandleToolCommand("tool.explode", nil, idx)
	if m, ok := out.(map[string]string); !ok || m["error"] == "" {
		t.Error("unknown cmd did not return error map")
	}
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{
		"present": "hello",
		"wrong":   42,
	}

	if GetString(m, "present", "def") != "hello" {
		t.Error("GetString failed on present string")
	}
	if GetString(m, "wrong", "def") != "def" {
		t.Error("GetString did not fallback on wrong type")
	}
	if GetString(m, "missing", "def") != "def" {
		t.Error("GetString did not fallback on missing key")
	}
	if GetString(nil, "x", "def") != "def" {
		t.Error("GetString did not handle nil map")
	}
}