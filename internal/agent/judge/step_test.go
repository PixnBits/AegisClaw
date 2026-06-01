// step_test.go — unit tests for the Judge step (Phase 1.4 hardening).

package judge

import (
	"context"
	"testing"

	"AegisClaw/internal/agent"
	agentSkills "AegisClaw/internal/agent/skills"
)

func TestJudge_UsesSkillIndexAndMemoryContext(t *testing.T) {
	skillIndex := agentSkills.NewAgentSkillIndex()

	tc := &agent.TurnContext{
		Input:              "User wants to add a skill.",
		SkillIndex:         skillIndex,
		CustomInstructions: "",
	}

	// Simulate memory context as it would appear after RunTurn's get_context call.
	tc.Input = map[string]interface{}{
		"original": tc.Input,
		"memory": map[string]interface{}{
			"short_term": []string{"User previously asked about governance."},
		},
	}

	llm := func(ctx context.Context, prompt string) (string, error) {
		return "Judged: High quality proposal. Should go to Court. Local tools and memory context were considered.", nil
	}

	result, err := Run(context.Background(), tc, llm)
	if err != nil {
		t.Fatalf("Judge.Run failed: %v", err)
	}

	if result == nil || result.Phase != "judge" {
		t.Errorf("expected judge phase, got %+v", result)
	}
}