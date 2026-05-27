// Package think implements phase 2 of the 6-step loop (per agent-runtime.md).
package think

import (
	"context"
	"fmt"

	"AegisClaw/internal/agent"
	agentSkills "AegisClaw/internal/agent/skills"
)

func Run(ctx context.Context, tc *agent.TurnContext, llm agent.LLMCallFunc) (*agent.StepResult, error) {
	input := fmt.Sprintf("%v", tc.Input)
	available := agentSkills.FormatAvailableTools(tc.SkillIndex, nil)
	custom := tc.CustomInstructions
	prompt := custom + "Think step-by-step about the observed request using the memory context from this turn. Identify risks, required skills/tools, autonomy implications. Available local tools: " + available + ". Context + Request: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "think", Content: text}, nil
}
