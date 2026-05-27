// Package plan implements phase 3 (concrete plan, only tools from local index).
package plan

import (
	"context"
	"fmt"

	"AegisClaw/internal/agent"
)

func Run(ctx context.Context, tc *agent.TurnContext, llm agent.LLMCallFunc) (*agent.StepResult, error) {
	input := fmt.Sprintf("%v", tc.Input)
	available := ""
	custom := tc.CustomInstructions
	prompt := custom + "Create a concrete plan: steps, which tools/skills via Hub (only use ones from the available local index), whether to create a formal proposal for Court review. Be specific. Available tools: " + available + ". Request: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "plan", Content: text}, nil
}
