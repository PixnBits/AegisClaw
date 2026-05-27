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
	input := fmt.Sprintf("%v", tc.Input)
	available := "" // TODO in 1.1b follow-up: use skills.FormatAvailableTools(tc.SkillIndex, ...)
	custom := tc.CustomInstructions
	prompt := custom + "Observe and parse the user/agent request. Extract intent, key entities, and whether this requires a proposal (e.g. new skill). Available local tools/skills: " + available + ". Input: " + input + ". Return structured observation."

	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{
		Phase:   "observe",
		Content: text,
	}, nil
}
