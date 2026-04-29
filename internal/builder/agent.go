package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// PipelineExecutor defines the interface for pipeline execution.
// This allows the BuilderAgent to work with different pipeline implementations
// (e.g., full Pipeline with nested VMs, or simplified in-VM pipeline).
type PipelineExecutor interface {
	Execute(ctx context.Context, prop *proposal.Proposal, spec *SkillSpec) (*PipelineResult, error)
}

// BuilderAgent runs inside a microVM and handles build requests.
// It polls for proposals in "implementing" status and executes the pipeline.
type BuilderAgent struct {
	pipeline     PipelineExecutor
	store        *proposal.Store
	kernel       *kernel.Kernel
	logger       *zap.Logger
	pollInterval time.Duration
	stopCh       chan struct{}
}

// NewBuilderAgent creates a builder agent for use inside a microVM.
func NewBuilderAgent(
	pipeline PipelineExecutor,
	store *proposal.Store,
	kern *kernel.Kernel,
	logger *zap.Logger,
) *BuilderAgent {
	return &BuilderAgent{
		pipeline:     pipeline,
		store:        store,
		kernel:       kern,
		logger:       logger,
		pollInterval: 10 * time.Second,
		stopCh:       make(chan struct{}),
	}
}

// Run starts the builder agent's main loop.
// It polls for proposals in "implementing" status and builds them.
func (ba *BuilderAgent) Run(ctx context.Context) error {
	ba.logger.Info("builder agent starting",
		zap.Duration("poll_interval", ba.pollInterval),
	)

	ticker := time.NewTicker(ba.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			ba.logger.Info("builder agent shutting down (context cancelled)")
			return ctx.Err()
		case <-ba.stopCh:
			ba.logger.Info("builder agent shutting down (stop signal)")
			return nil
		case <-ticker.C:
			if err := ba.checkAndBuild(ctx); err != nil {
				ba.logger.Error("error checking for builds", zap.Error(err))
			}
		}
	}
}

// checkAndBuild finds proposals that need building and processes them.
func (ba *BuilderAgent) checkAndBuild(ctx context.Context) error {
	summaries, err := ba.store.List()
	if err != nil {
		return fmt.Errorf("failed to list proposals: %w", err)
	}

	for _, summary := range summaries {
		if summary.Status != proposal.StatusImplementing {
			continue
		}

		// Load full proposal
		prop, err := ba.store.Get(summary.ID)
		if err != nil {
			ba.logger.Error("failed to get proposal",
				zap.String("proposal_id", summary.ID),
				zap.Error(err),
			)
			continue
		}

		// Build this proposal
		if err := ba.buildProposal(ctx, prop); err != nil {
			ba.logger.Error("failed to build proposal",
				zap.String("proposal_id", prop.ID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// buildProposal executes the build pipeline for a single proposal.
func (ba *BuilderAgent) buildProposal(ctx context.Context, prop *proposal.Proposal) error {
	ba.logger.Info("building proposal",
		zap.String("proposal_id", prop.ID),
		zap.String("title", prop.Title),
	)

	// Log build start to kernel
	action := kernel.NewAction(
		kernel.ActionType("builder.start"),
		"builder-agent",
		[]byte(fmt.Sprintf(`{"proposal_id":"%s","title":"%s"}`, prop.ID, prop.Title)),
	)
	if _, err := ba.kernel.SignAndLog(action); err != nil {
		ba.logger.Warn("failed to log build start", zap.Error(err))
	}

	// Extract skill spec
	spec, err := extractSkillSpecFromProposal(prop)
	if err != nil {
		ba.markFailed(prop, fmt.Sprintf("invalid skill spec: %v", err))
		return err
	}

	// Execute pipeline
	startTime := time.Now()
	result, err := ba.pipeline.Execute(ctx, prop, spec)
	duration := time.Since(startTime)

	if err != nil {
		ba.markFailed(prop, fmt.Sprintf("pipeline execution failed: %v", err))
		return err
	}

	// Log completion
	action = kernel.NewAction(
		kernel.ActionType("builder.complete"),
		"builder-agent",
		[]byte(fmt.Sprintf(`{"proposal_id":"%s","state":"%s","duration_ms":%d}`,
			prop.ID, result.State, duration.Milliseconds())),
	)
	if _, err := ba.kernel.SignAndLog(action); err != nil {
		ba.logger.Warn("failed to log build completion", zap.Error(err))
	}

	// Update proposal status
	if result.State == PipelineStateComplete {
		if err := prop.Transition(proposal.StatusComplete, "build completed successfully", "builder-agent"); err != nil {
			return fmt.Errorf("failed to transition to complete: %w", err)
		}
		if err := ba.store.Update(prop); err != nil {
			return fmt.Errorf("failed to update proposal: %w", err)
		}
	} else {
		ba.markFailed(prop, fmt.Sprintf("pipeline state: %s, error: %s", result.State, result.Error))
	}

	return nil
}

// markFailed transitions a proposal to failed status.
func (ba *BuilderAgent) markFailed(prop *proposal.Proposal, reason string) {
	if err := prop.Transition(proposal.StatusFailed, reason, "builder-agent"); err != nil {
		ba.logger.Error("failed to transition to failed",
			zap.String("proposal_id", prop.ID),
			zap.Error(err),
		)
		return
	}

	if err := ba.store.Update(prop); err != nil {
		ba.logger.Error("failed to update failed proposal",
			zap.String("proposal_id", prop.ID),
			zap.Error(err),
		)
	}
}

// Stop signals the agent to shut down gracefully.
func (ba *BuilderAgent) Stop() {
	close(ba.stopCh)
}

// HandleBuildRequest processes a build request sent via vsock.
// This is called by the vsock handler when a BuildRequest arrives.
func (ba *BuilderAgent) HandleBuildRequest(ctx context.Context, req *BuildRequest) (*BuildResponse, error) {
	ba.logger.Info("received build request",
		zap.String("proposal_id", req.ProposalID),
		zap.String("title", req.Title),
	)

	// Load the proposal
	prop, err := ba.store.Get(req.ProposalID)
	if err != nil {
		return &BuildResponse{
			ProposalID: req.ProposalID,
			State:      PipelineStateFailed,
			Error:      fmt.Sprintf("failed to load proposal: %v", err),
		}, nil
	}

	// Extract skill spec
	var spec *SkillSpec
	if len(req.Spec) > 0 {
		var s SkillSpec
		if err := json.Unmarshal(req.Spec, &s); err != nil {
			return &BuildResponse{
				ProposalID: req.ProposalID,
				State:      PipelineStateFailed,
				Error:      fmt.Sprintf("invalid skill spec: %v", err),
			}, nil
		}
		spec = &s
	} else {
		spec, err = extractSkillSpecFromProposal(prop)
		if err != nil {
			return &BuildResponse{
				ProposalID: req.ProposalID,
				State:      PipelineStateFailed,
				Error:      fmt.Sprintf("failed to extract skill spec: %v", err),
			}, nil
		}
	}

	// Execute pipeline
	result, err := ba.pipeline.Execute(ctx, prop, spec)
	if err != nil {
		return &BuildResponse{
			ProposalID: req.ProposalID,
			State:      PipelineStateFailed,
			Error:      err.Error(),
			Round:      req.Round,
		}, nil
	}

	// Convert PipelineResult to BuildResponse
	return &BuildResponse{
		ProposalID: result.ProposalID,
		State:      result.State,
		CommitHash: result.CommitHash,
		Branch:     result.Branch,
		Files:      result.Files,
		FileHashes: result.FileHashes,
		Reasoning:  result.Reasoning,
		Error:      result.Error,
		Round:      result.Round,
	}, nil
}

// extractSkillSpecFromProposal creates a SkillSpec from a proposal.
func extractSkillSpecFromProposal(prop *proposal.Proposal) (*SkillSpec, error) {
	if len(prop.Spec) == 0 {
		// Create default spec
		return &SkillSpec{
			Name:        prop.TargetSkill,
			Description: prop.Description,
			Language:    "go",
			NetworkPolicy: SkillNetworkPolicy{
				DefaultDeny: true,
			},
		}, nil
	}

	// Unmarshal from proposal
	var spec SkillSpec
	if err := json.Unmarshal(prop.Spec, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill spec: %w", err)
	}

	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid skill spec: %w", err)
	}

	return &spec, nil
}
