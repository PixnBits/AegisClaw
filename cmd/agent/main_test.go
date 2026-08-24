package main

import (
	"strings"
	"testing"
)

func TestMentionedProseIfDropped(t *testing.T) {
	if got := mentionedProseIfDropped("PASS"); got != "" {
		t.Fatalf("PASS must stay silent, got %q", got)
	}
	if got := mentionedProseIfDropped("I'll bump the button padding 2px."); got == "" {
		t.Fatal("bare assignment ack must post when @mentioned")
	}
	if got := mentionedProseIfDropped("@ProjectManager which repo and path?"); got == "" {
		t.Fatal("asking for repo/path must post when @mentioned")
	}
}

func TestLooksLikeProgressClaim(t *testing.T) {
	if !looksLikeProgressClaim("I'll adjust the login button padding to fix the 2px offset issue.") {
		t.Fatal("claimed CSS tweak without a path must count as invented progress")
	}
	if looksLikeProgressClaim("I can take this as Coder — @ProjectManager, which repo and path? I have not changed any code yet.") {
		t.Fatal("asking for repo/path must not count as a progress claim")
	}
	if batchNamesWorkTarget("from: user: The login button padding is 2px off. CSS only.", "") {
		t.Fatal("CSS goal with no path must not look like a work target")
	}
	if !batchNamesWorkTarget("from: project-manager: @Coder tweak web-portal/src/LoginButton.css padding.", "") {
		t.Fatal("named file path must count as a work target")
	}
}

func TestLooksLikeInternalDump(t *testing.T) {
	dump := `{
  "intent": "request_information",
  "entities": {"request_type": "repository_and_file_path"},
  "requires_proposal": false,
  "tool_calls": [],
  "observation": {"summary": "User is requesting information"}
}`
	if !looksLikeInternalDump(dump) {
		t.Fatal("skill-planner JSON must not be posted to the channel")
	}
	if got := mentionedProseIfDropped(dump); got != "" {
		t.Fatalf("dump must not recover as mention prose, got %q", got)
	}
	if looksLikeInternalDump("I can take this as Tester — @ProjectManager, which repo and path?") {
		t.Fatal("plain ask must not look like an internal dump")
	}
	md := "## Request Analysis\nThe user is asking for the repository.\n## Required Skills/Tools\n1. **discord_monitor**"
	if !looksLikeInternalDump(md) {
		t.Fatal("markdown skill-planner dump must not be posted")
	}
}

func TestAgentSkillIndex_ListSkills(t *testing.T) {
	idx := NewAgentSkillIndex()
	skills := idx.ListSkills()
	if len(skills) == 0 {
		t.Fatal("expected seeded skills")
	}
	found := false
	for _, s := range skills {
		if s.ID == "discord_monitor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected discord_monitor skill to be present")
	}
}

func TestAgentSkillIndex_SearchTools_Basic(t *testing.T) {
	idx := NewAgentSkillIndex()

	results := idx.SearchTools("send message discord", 5)
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'send message discord'")
	}

	// Best result should be the discord send tool
	top := results[0]
	if !strings.Contains(strings.ToLower(top.Tool.Name), "discord") ||
		!strings.Contains(strings.ToLower(top.Tool.Description), "message") {
		t.Errorf("top result did not look like discord send: %+v", top.Tool)
	}
	if top.Score < 0.3 {
		t.Errorf("expected reasonably high score, got %f", top.Score)
	}
}

func TestAgentSkillIndex_SearchTools_Semanticish(t *testing.T) {
	idx := NewAgentSkillIndex()

	// Natural language query that doesn't contain exact tool name
	results := idx.SearchTools("post something to chat on discord", 3)
	if len(results) == 0 {
		t.Fatal("expected results for natural language discord query")
	}

	foundDiscord := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.Tool.Name), "discord") {
			foundDiscord = true
			break
		}
	}
	if !foundDiscord {
		t.Error("semantic-ish search should still surface discord tools")
	}
}

func TestAgentSkillIndex_SearchTools_NoResults(t *testing.T) {
	idx := NewAgentSkillIndex()
	results := idx.SearchTools("completely unrelated quantum teleportation blockchain", 5)
	// We may get weak matches; just ensure it doesn't panic and returns something reasonable
	if len(results) > 5 {
		t.Error("should respect limit")
	}
}
