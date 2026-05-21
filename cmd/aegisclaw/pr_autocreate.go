package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"go.uber.org/zap"
)

const (
	// riskThresholdForApproval is the maximum average risk score for Court code review approval.
	// Reviews with average risk >= this threshold are rejected.
	riskThresholdForApproval = 5.0
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
		truncateCommitHash(commitHash),
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
		"pr_id":           pr.ID,
		"proposal_id":     proposalID,
		"branch":          branch,
		"commit":          commitHash,
		"files":           pr.FilesChanged,
		"analysis_passed": pr.AnalysisPassed,
		"security_passed": pr.SecurityGatesPassed,
	})
	action := kernel.NewAction(kernel.ActionType("pr.create"), "pipeline", auditPayload)
	if _, err := env.Kernel.SignAndLog(action); err != nil {
		env.Logger.Warn("failed to log PR creation", zap.Error(err))
	}

	env.Logger.Info("pull request created from pipeline",
		zap.String("pr_id", pr.ID),
		zap.String("proposal_id", proposalID),
		zap.String("branch", branch),
		zap.String("commit", truncateCommitHash(commitHash)),
		zap.Int("files", pr.FilesChanged),
	)

	// Request Court code review via CourtClient (real review logic lives in Court VMs / Scribe).
	if env.CourtClient != nil {
		if err := env.CourtClient.Review(context.Background(), pr.ID); err != nil {
			env.Logger.Warn("Court review request failed (Court components may be unavailable)",
				zap.String("pr_id", pr.ID), zap.Error(err))
		}
	}
}

// triggerCourtCodeReview has been removed from the Host Daemon TCB.
// Real Court reviews now happen in Court VMs / Court Scribe.
func triggerCourtCodeReview(env *runtimeEnv, pr *pullrequest.PullRequest, result *builder.PipelineResult) {
	// Build the code review request from the pipeline result
	codeReq := &court.CodeReviewRequest{
		PRID:                pr.ID,
		ProposalID:          pr.ProposalID,
		Title:               pr.Title,
		Description:         pr.Description,
		Branch:              pr.Branch,
		CommitHash:          pr.CommitHash,
		Files:               result.Files,
		FilesChanged:        pr.FilesChanged,
		Additions:           pr.Additions,
		Deletions:           pr.Deletions,
		BuildPassed:         pr.BuildPassed,
		AnalysisPassed:      pr.AnalysisPassed,
		SecurityGatesPassed: pr.SecurityGatesPassed,
	}

	// Validate the request before sending to Court
	if err := codeReq.Validate(); err != nil {
		env.Logger.Error("invalid code review request",
			zap.String("pr_id", pr.ID),
			zap.Error(err),
		)
		// Mark Court review as failed
		pr.CourtReviewStatus = pullrequest.CourtReviewRejected
		if err := env.PRStore.Update(pr); err != nil {
			env.Logger.Error("failed to update PR status", zap.Error(err))
		}
		return
	}

	// Update status to in-progress
	pr.CourtReviewStatus = pullrequest.CourtReviewInProgress
	if err := env.PRStore.Update(pr); err != nil {
		env.Logger.Warn("failed to update PR status to in-progress", zap.Error(err))
	}

	env.Logger.Info("triggering Court code review",
		zap.String("pr_id", pr.ID),
		zap.Int("files", len(result.Files)),
	)

	// Court code review request is now handled via CourtClient (no direct Engine access).
	go func() {
		if env.CourtClient == nil {
			return
		}
		ctx := context.Background()
		if err := env.CourtClient.Review(ctx, pr.ID); err != nil {
			env.Logger.Warn("Court review request via CourtClient failed (expected during transition)",
				zap.String("pr_id", pr.ID), zap.Error(err))
		}
		// PR status updates etc. can be driven by Court Scribe callbacks in the future.
	}()
}

// boolToPassFail converts a boolean to "PASS" or "FAIL" string.
func boolToPassFail(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}

// truncateCommitHash returns the first 12 characters of a commit hash.
// If the hash is shorter than 12 characters, returns the full hash.
func truncateCommitHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
