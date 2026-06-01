// Package execute implements phase 5 (actually perform the Hub calls for tools/skills).
// In 1.1b this is still LLM-mediated; later slices will parse structured actions
// and truly dispatch skill.* messages via the hubclient (the "exclusively through AegisHub" requirement).
package execute

import (
	"context"
	"fmt"

	"AegisClaw/internal/agent"
	agentSkills "AegisClaw/internal/agent/skills"
)

func Run(ctx context.Context, tc *agent.TurnContext, llm agent.LLMCallFunc) (*agent.StepResult, error) {
	// Phase 3 enforcement point (governance-court.md §Court Process + agent-runtime.md):
	// Before any execution, fail-closed on Court-revoked scopes.
	// In a real implementation every tool in the plan would be checked here.
	if agent.IsScopeRevoked(tc, "any.privileged") {
		return &agent.StepResult{
			Phase:   "execute",
			Content: "BLOCKED: Court decision has revoked required scopes for this action (fail-closed per security-model.md)",
		}, nil
	}

	input := fmt.Sprintf("%v", tc.Input)
	available := agentSkills.FormatAvailableTools(tc.SkillIndex, nil)
	custom := tc.CustomInstructions
	prompt := custom + "Perform the execution: actually send signed tool/skill calls to Hub (only use tools from the available local index) or invoke proposal creation flow. Capture results. Available: " + available + ". Request: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "execute", Content: text}, nil
}
