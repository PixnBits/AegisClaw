// Package act implements phase 4 (prepare signed invocations or proposal payload).
package act

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
	prompt := custom + "Execute the 'Act' phase: prepare specific tool invocations (signed via Hub, only from available local index) or proposal payload. If skill creation, prepare for proposal.create. Available tools: " + available + ". Request: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "act", Content: text}, nil
}
