// Package judge implements the final phase (quality, policy compliance, Court trigger).
// It is the governance gate before any external action.
package judge

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
	prompt := custom + "Judge the response quality, compliance with policy, and whether Court review is required. Available local tools: " + available + ". Payload: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "judge", Content: text}, nil
}
