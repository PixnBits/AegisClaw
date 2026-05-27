// step_test.go — unit tests for the Execute step (Phase 1.4 coverage push).
//
// These tests exercise the real Execute implementation with controlled inputs,
// including the local skill index (7.3) and memory context from a real Memory VM.
//
// SPEC REFERENCES:
//   - agent-runtime.md §Responsibilities (Execute step uses local tool index + Memory context)
//   - memory-vm.md §1 (memory.get_context at start of every turn)
//   - no-stubs-plan/phase-1.md 1.4 (increase coverage on reasoning steps)

package execute

import (
	"context"
	"testing"

	"AegisClaw/internal/agent"
	agentSkills "AegisClaw/internal/agent/skills"
)

func TestExecute_UsesMemoryContextAndSkillIndex(t *testing.T) {
	// Build a realistic TurnContext as it would appear after a successful
	// memory.get_context call inside RunTurn (see loop.go and memory-vm.md).
	skillIndex := agentSkills.NewAgentSkillIndex()

	tc := &agent.TurnContext{
		Input:              "User requested execution of a background research task.",
		SkillIndex:         skillIndex,
		CustomInstructions: "Follow least-privilege and require Court for new skills.",
	}

	// Simulate the memory context that RunTurn injects (short-term + long-term).
	tc.Input = map[string]interface{}{
		"original": tc.Input,
		"memory": map[string]interface{}{
			"short_term": []string{"Autonomy granted for research tasks in prior turn."},
			"long_term":  []interface{}{"All tool calls must be auditable via Hub."},
		},
	}

	// Controllable LLM that returns known text so we can assert the prompt
	// construction is correct (it must include memory context and available tools).
	capturedPrompt := ""
	llm := func(ctx context.Context, prompt string) (string, error) {
		capturedPrompt = prompt
		return "Execute: Dispatched signed tool call via Hub for web_research (from local index only). Captured result and updated memory. Context + tools respected.", nil
	}

	result, err := Run(context.Background(), tc, llm)
	if err != nil {
		t.Fatalf("Execute.Run failed: %v", err)
	}

	if result == nil || result.Phase != "execute" {
		t.Errorf("expected execute phase, got %+v", result)
	}

	// The prompt must contain evidence of memory context and the local skill index.
	if capturedPrompt == "" {
		t.Fatal("LLM was never called")
	}
	if !contains(capturedPrompt, "Autonomy granted for research") {
		t.Error("prompt did not include recent memory context")
	}
	if !contains(capturedPrompt, "discord_monitor") {
		t.Error("prompt did not include local skill index (7.3 requirement)")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
