// Package observe implements the first phase of the 6-step Agent Runtime loop.
//
// SPEC: docs/specs/agent-runtime.md §Responsibilities (Observe: extract intent, entities,
// whether a proposal is required, using local tool index + Memory context).

package observe

import (
	"context"
	"fmt"

	"AegisClaw/internal/agent"
)

func Run(ctx context.Context, tc *agent.TurnContext, llm agent.LLMCallFunc) (*agent.StepResult, error) {
	// Incorporate memory context from real Memory VM (memory-vm.md §1 + §Communication Interface)
	memoryContext := extractMemoryContextForPrompt(tc)

	available := "" // TODO: wire skills.FormatAvailableTools properly
	custom := tc.CustomInstructions

	prompt := custom + "Observe and parse the user/agent request. Use the provided recent conversation history and relevant long-term memories. Extract intent, key entities, and whether this requires a proposal (e.g. new skill). Available local tools/skills: " + available + ". Memory context: " + memoryContext + ". Current input: " + fmt.Sprintf("%v", tc.Input) + ". Return structured observation."

	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{
		Phase:   "observe",
		Content: text,
	}, nil
}

// extractMemoryContextForPrompt pulls short-term history + long-term notes from the
// structure that loop.RunTurn injects after a successful memory.get_context call
// against a real Memory VM.
func extractMemoryContextForPrompt(tc *agent.TurnContext) string {
	if tc == nil || tc.Input == nil {
		return "(no memory context available)"
	}

	m, ok := tc.Input.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", tc.Input)
	}

	mem, ok := m["memory"].(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", tc.Input)
	}

	short := mem["short_term"]
	long := mem["long_term"]

	return fmt.Sprintf("Recent history: %v. Relevant long-term: %v", short, long)
}
