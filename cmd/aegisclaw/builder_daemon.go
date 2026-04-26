package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// builderDaemon watches for proposals in "implementing" status and triggers
// the builder pipeline to generate code. This is the critical link between
// Court approval (Phase 2) and code generation (Phase 3) in the SDLC flow.
type builderDaemon struct {
	env          *runtimeEnv
	pipeline     *builder.Pipeline
	gitMgr       *gitmanager.Manager
	activeBuild  sync.Map // proposalID -> bool (prevents duplicate builds)
	stopCh       chan struct{}
	wg           sync.WaitGroup
	pollInterval time.Duration
}

// startBuilderDaemon initializes and starts the builder daemon that monitors
// for approved proposals and triggers code generation.
func startBuilderDaemon(ctx context.Context, env *runtimeEnv) error {
	// Validate configuration
	if env.Config.Builder.WorkspaceBaseDir == "" {
		env.Logger.Warn("builder workspace directory not configured, builder daemon disabled")
		return nil
	}
	if env.Config.Builder.RootfsTemplate == "" {
		env.Logger.Warn("builder rootfs template not configured, builder daemon disabled")
		return nil
	}

	env.Logger.Info("initializing builder daemon",
		zap.String("workspace_dir", env.Config.Builder.WorkspaceBaseDir),
		zap.String("rootfs_template", env.Config.Builder.RootfsTemplate),
		zap.Int("max_concurrent", env.Config.Builder.MaxConcurrentBuilds),
	)

	// Initialize git manager for workspace
	gitMgr, err := initBuilderGitManager(env)
	if err != nil {
		return fmt.Errorf("failed to initialize git manager: %w", err)
	}

	// Initialize builder subsystem components
	pipeline, err := initBuilderPipeline(env, gitMgr)
	if err != nil {
		return fmt.Errorf("failed to initialize builder pipeline: %w", err)
	}

	// Set PR creation callback to integrate with Phase 4
	pipeline.SetPRCreatedCallback(func(proposalID, branch, commitHash string, result *builder.PipelineResult) {
		createPRFromPipelineResult(env, proposalID, branch, commitHash, result)
	})

	// Configure SBOM directory if available
	if env.Config.Builder.SBOMDir != "" {
		pipeline.SetSBOMDir(env.Config.Builder.SBOMDir)
	}

	// Create and start daemon
	daemon := &builderDaemon{
		env:          env,
		pipeline:     pipeline,
		gitMgr:       gitMgr,
		stopCh:       make(chan struct{}),
		pollInterval: 10 * time.Second,
	}

	daemon.wg.Add(1)
	go daemon.run(ctx)

	env.Logger.Info("builder daemon started successfully")
	return nil
}

// run is the main daemon loop that polls for implementing proposals
func (d *builderDaemon) run(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	d.env.Logger.Info("builder daemon polling started",
		zap.Duration("interval", d.pollInterval),
	)

	for {
		select {
		case <-ctx.Done():
			d.env.Logger.Info("builder daemon shutting down (context cancelled)")
			return
		case <-d.stopCh:
			d.env.Logger.Info("builder daemon shutting down (stop signal)")
			return
		case <-ticker.C:
			d.checkAndBuildProposals(ctx)
		}
	}
}

// checkAndBuildProposals finds proposals in "implementing" status and triggers builds
func (d *builderDaemon) checkAndBuildProposals(ctx context.Context) {
	summaries, err := d.env.ProposalStore.List()
	if err != nil {
		d.env.Logger.Error("failed to list proposals", zap.Error(err))
		return
	}

	for _, summary := range summaries {
		// Only process proposals in implementing status
		if summary.Status != proposal.StatusImplementing {
			continue
		}

		// Skip if already building
		if _, building := d.activeBuild.Load(summary.ID); building {
			continue
		}

		// Load full proposal
		prop, err := d.env.ProposalStore.Get(summary.ID)
		if err != nil {
			d.env.Logger.Error("failed to get proposal",
				zap.String("proposal_id", summary.ID),
				zap.Error(err),
			)
			continue
		}

		// Mark as building to prevent duplicate triggers
		d.activeBuild.Store(summary.ID, true)

		// Trigger build in background goroutine
		go d.buildProposal(ctx, prop)
	}
}

// buildProposal executes the builder pipeline for a single proposal
func (d *builderDaemon) buildProposal(ctx context.Context, prop *proposal.Proposal) {
	defer d.activeBuild.Delete(prop.ID)

	d.env.Logger.Info("starting builder pipeline for proposal",
		zap.String("proposal_id", prop.ID),
		zap.String("title", prop.Title),
		zap.String("category", string(prop.Category)),
	)

	// Log build start to kernel audit trail
	action := kernel.NewAction(
		kernel.ActionType("builder.start"),
		"builder-daemon",
		[]byte(fmt.Sprintf(`{"proposal_id":"%s","title":"%s"}`, prop.ID, prop.Title)),
	)
	if _, err := d.env.Kernel.SignAndLog(action); err != nil {
		d.env.Logger.Warn("failed to log builder start", zap.Error(err))
	}

	// Extract skill spec from proposal
	spec, err := extractSkillSpec(prop)
	if err != nil {
		d.env.Logger.Error("failed to extract skill spec from proposal",
			zap.String("proposal_id", prop.ID),
			zap.Error(err),
		)
		d.markProposalFailed(prop, fmt.Sprintf("invalid skill spec: %v", err))
		return
	}

	// Execute the pipeline
	startTime := time.Now()
	result, err := d.pipeline.Execute(ctx, prop, spec)
	duration := time.Since(startTime)

	if err != nil {
		d.env.Logger.Error("builder pipeline failed",
			zap.String("proposal_id", prop.ID),
			zap.Error(err),
			zap.Duration("duration", duration),
		)
		d.markProposalFailed(prop, fmt.Sprintf("pipeline execution failed: %v", err))
		return
	}

	// Log pipeline completion
	d.env.Logger.Info("builder pipeline completed",
		zap.String("proposal_id", prop.ID),
		zap.String("state", string(result.State)),
		zap.String("commit_hash", result.CommitHash),
		zap.String("branch", result.Branch),
		zap.Int("files", len(result.Files)),
		zap.Duration("duration", duration),
	)

	// Log result to kernel audit trail
	action = kernel.NewAction(
		kernel.ActionType("builder.complete"),
		"builder-daemon",
		[]byte(fmt.Sprintf(`{"proposal_id":"%s","state":"%s","commit":"%s","duration_ms":%d}`,
			prop.ID, result.State, result.CommitHash, duration.Milliseconds())),
	)
	if _, err := d.env.Kernel.SignAndLog(action); err != nil {
		d.env.Logger.Warn("failed to log builder completion", zap.Error(err))
	}

	// Update proposal status based on result
	if result.State == builder.PipelineStateComplete {
		// Transition to complete - build succeeded
		if err := prop.Transition(proposal.StatusComplete, "builder pipeline completed successfully", "builder-daemon"); err != nil {
			d.env.Logger.Error("failed to transition proposal to complete",
				zap.String("proposal_id", prop.ID),
				zap.Error(err),
			)
		} else {
			if err := d.env.ProposalStore.Update(prop); err != nil {
				d.env.Logger.Error("failed to update proposal",
					zap.String("proposal_id", prop.ID),
					zap.Error(err),
				)
			}
		}
	} else {
		// Build failed or was cancelled
		d.markProposalFailed(prop, fmt.Sprintf("pipeline state: %s, error: %s", result.State, result.Error))
	}
}

// markProposalFailed transitions a proposal to failed status with a reason
func (d *builderDaemon) markProposalFailed(prop *proposal.Proposal, reason string) {
	if err := prop.Transition(proposal.StatusFailed, reason, "builder-daemon"); err != nil {
		d.env.Logger.Error("failed to transition proposal to failed",
			zap.String("proposal_id", prop.ID),
			zap.String("reason", reason),
			zap.Error(err),
		)
		return
	}

	if err := d.env.ProposalStore.Update(prop); err != nil {
		d.env.Logger.Error("failed to update failed proposal",
			zap.String("proposal_id", prop.ID),
			zap.Error(err),
		)
	}

	d.env.Logger.Error("proposal marked as failed",
		zap.String("proposal_id", prop.ID),
		zap.String("reason", reason),
	)
}

// extractSkillSpec converts a proposal to a SkillSpec for the builder
func extractSkillSpec(prop *proposal.Proposal) (*builder.SkillSpec, error) {
	// Parse the proposal's Spec field as a SkillSpec
	if len(prop.Spec) == 0 {
		// If no spec provided, create a basic one from the proposal metadata
		return &builder.SkillSpec{
			Name:        prop.TargetSkill,
			Description: prop.Description,
			Language:    "go", // Default to Go
			NetworkPolicy: builder.SkillNetworkPolicy{
				DefaultDeny: true,
			},
		}, nil
	}

	// Unmarshal the spec
	var spec builder.SkillSpec
	if err := json.Unmarshal(prop.Spec, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill spec: %w", err)
	}

	// Validate the spec
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid skill spec: %w", err)
	}

	return &spec, nil
}

// initBuilderGitManager creates a git manager for the builder workspace
func initBuilderGitManager(env *runtimeEnv) (*gitmanager.Manager, error) {
	workspaceDir := env.Config.Builder.WorkspaceBaseDir
	if workspaceDir == "" {
		workspaceDir = filepath.Join(env.Config.Sandbox.StateDir, "builder-workspace")
	}

	mgr, err := gitmanager.NewManager(workspaceDir, env.Kernel, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create git manager: %w", err)
	}

	return mgr, nil
}

// initBuilderPipeline creates the full builder pipeline with all dependencies
func initBuilderPipeline(env *runtimeEnv, gitMgr *gitmanager.Manager) (*builder.Pipeline, error) {
	// Create builder configuration
	builderCfg := builder.BuilderConfig{
		RootfsTemplate:      env.Config.Builder.RootfsTemplate,
		WorkspaceBaseDir:    env.Config.Builder.WorkspaceBaseDir,
		MaxConcurrentBuilds: env.Config.Builder.MaxConcurrentBuilds,
		BuildTimeout:        time.Duration(env.Config.Builder.BuildTimeoutMinutes) * time.Minute,
	}

	if builderCfg.MaxConcurrentBuilds == 0 {
		builderCfg.MaxConcurrentBuilds = 2
	}
	if builderCfg.BuildTimeout == 0 {
		builderCfg.BuildTimeout = 10 * time.Minute
	}

	// Initialize BuilderRuntime
	builderRT, err := builder.NewBuilderRuntime(builderCfg, env.Runtime, env.Kernel, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder runtime: %w", err)
	}

	// Load prompt templates
	templates := builder.DefaultTemplates()

	// Initialize CodeGenerator
	codeGen, err := builder.NewCodeGenerator(builderRT, env.Kernel, env.Logger, templates)
	if err != nil {
		return nil, fmt.Errorf("failed to create code generator: %w", err)
	}

	// Initialize Analyzer
	analyzer, err := builder.NewAnalyzer(builderRT, env.Kernel, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create analyzer: %w", err)
	}

	// Create the pipeline
	pipeline, err := builder.NewPipeline(
		builderRT,
		codeGen,
		gitMgr,
		analyzer,
		env.Kernel,
		env.ProposalStore,
		env.Logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}

	return pipeline, nil
}

// stop gracefully stops the builder daemon
func (d *builderDaemon) stop() {
	close(d.stopCh)
	d.wg.Wait()
}
