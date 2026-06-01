package main

import (
	"strings"
	"testing"
)

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
