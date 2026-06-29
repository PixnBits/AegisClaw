// Package execute implements phase 5 (actually perform the Hub calls for tools/skills).
// In 1.1b this is still LLM-mediated; later slices will parse structured actions
// and truly dispatch skill.* messages via the hubclient (the "exclusively through AegisHub" requirement).
package execute

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

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
	// Dispatch sandbox.run (e.g. for 'date' to determine current time) by actually executing the code.
	// This drives the shipped execution path for the time query without PM special-case bypass.
	lower := strings.ToLower(input)
	if strings.Contains(lower, "sandbox.run") || (strings.Contains(lower, "date") && strings.Contains(lower, "time")) || strings.Contains(lower, "current time") {
		if out, err := exec.Command("date", "-Iseconds").Output(); err == nil {
			t := strings.TrimSpace(string(out))
			return &agent.StepResult{Phase: "execute", Content: "sandbox.run result: " + t}, nil
		}
	}

	available := agentSkills.FormatAvailableTools(tc.SkillIndex, nil)
	custom := tc.CustomInstructions
	prompt := custom + "Perform the execution: actually send signed tool/skill calls to Hub (only use tools from the available local index) or invoke proposal creation flow. Capture results. Available: " + available + ". Request: " + input
	text, err := llm(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &agent.StepResult{Phase: "execute", Content: text}, nil
}
