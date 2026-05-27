// Package think implements phase 2 of the 6-step loop (per agent-runtime.md).
package think

import (
	"context"
	"fmt"

	"AegisClaw/internal/agent"
)

func Run(ctx context.Context, tc *agent.TurnContext, llm agent.LLMCallFunc) (*agent.StepResult, error) {
	input := fmt.Sprintf("%v", tc.Input)
	available := ""
	custom := tc.CustomInstructions
	prompt := custom + "Think step-by-step about the observed request using prior context. Identify risks, required skills/tools, autonomy implications. Available local tools you can actually call: " + available + ". Request: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "think", Content: text}, nil
}
