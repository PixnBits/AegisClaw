package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"go.uber.org/zap"
)

// createPRFromPipelineResult creates a pull request from a completed builder pipeline.
// This is called automatically when the builder completes successfully.
func createPRFromPipelineResult(env *runtimeEnv, proposalID, branch, commitHash string, result *builder.PipelineResult) {
	if env.PRStore == nil {
		env.Logger.Warn("PR store not available, skipping auto-create")
		return
	}
	
	// Get the proposal to extract metadata
	prop, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		env.Logger.Error("failed to get proposal for PR creation",
			zap.String("proposal_id", proposalID),
			zap.Error(err),
		)
		return
	}
	
	// Extract skill name from proposal's TargetSkill field
	skillName := prop.TargetSkill
	if skillName == "" {
		skillName = "unknown-skill"
	}
	
	// Generate PR title from proposal
	title := fmt.Sprintf("[%s] %s", skillName, prop.Title)
	
	// Create the base PR using the constructor
	pr, err := pullrequest.NewPullRequest(proposalID, title, "builder-pipeline", branch, commitHash)
	if err != nil {
		env.Logger.Error("failed to create pull request object",
			zap.String("proposal_id", proposalID),
			zap.Error(err),
		)
		return
	}
	
	// Set additional fields from pipeline result
	pr.Description = fmt.Sprintf(`Auto-generated pull request from builder pipeline.

**Proposal:** %s
**Skill:** %s
**Commit:** %s
**Branch:** %s → %s

## Changes
%s

## Build Results
- Files Changed: %d
- Analysis Result: %s
- Security Gates: %s

## Timeline
- Pipeline Started: %s
- Pipeline Completed: %s
- Duration: %s
`,
		proposalID,
		skillName,
		commitHash[:12],
		branch,
		pr.BaseBranch,
		prop.Description,
		len(result.Files),
		boolToPassFail(result.Analysis != nil),
		boolToPassFail(result.SecurityGateResult != nil && result.SecurityGateResult.Passed),
		result.StartedAt.Format(time.RFC3339),
		result.CompletedAt.Format(time.RFC3339),
		result.Duration.String(),
	)
	
	// Set build and security results
	pr.AnalysisPassed = result.Analysis != nil
	pr.SecurityGatesPassed = result.SecurityGateResult != nil && result.SecurityGateResult.Passed
	
	// Parse diff to get file stats
	pr.FilesChanged = len(result.Files)
	// Note: result.Diff is a string, not a structured diff object
	// We could parse it to count additions/deletions, but for now just set files changed
	
	// Always require Court review for generated code
	pr.CourtReviewRequired = true
	
	// Save the PR
	if err := env.PRStore.Create(pr); err != nil {
		env.Logger.Error("failed to create pull request",
			zap.String("proposal_id", proposalID),
			zap.String("branch", branch),
			zap.Error(err),
		)
		return
	}
	
	// Log PR creation to kernel audit trail
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"pr_id":            pr.ID,
		"proposal_id":      proposalID,
		"branch":           branch,
		"commit":           commitHash,
		"files":            pr.FilesChanged,
		"analysis_passed":  pr.AnalysisPassed,
		"security_passed":  pr.SecurityGatesPassed,
	})
	action := kernel.NewAction(kernel.ActionType("pr.create"), "pipeline", auditPayload)
	if _, err := env.Kernel.SignAndLog(action); err != nil {
		env.Logger.Warn("failed to log PR creation", zap.Error(err))
	}
	
	env.Logger.Info("pull request created from pipeline",
		zap.String("pr_id", pr.ID),
		zap.String("proposal_id", proposalID),
		zap.String("branch", branch),
		zap.String("commit", commitHash[:12]),
		zap.Int("files", pr.FilesChanged),
	)
	
	// TODO: Trigger Court code review for the generated code
	// This would call the Court engine to review the actual code changes
	// in the PR, not just the proposal.
	// For now, we log that this should happen.
	env.Logger.Info("TODO: trigger Court code review for PR",
		zap.String("pr_id", pr.ID),
		zap.String("proposal_id", proposalID),
	)
}

// boolToPassFail converts a boolean to "PASS" or "FAIL" string.
func boolToPassFail(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}
