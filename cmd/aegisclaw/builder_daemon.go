package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	gozap "go.uber.org/zap"
)

var builderDispatchInFlight sync.Map

func startBuilderDispatchDaemon(ctx context.Context, env *runtimeEnv) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		env.Logger.Info("builder dispatch daemon started")
		for {
			select {
			case <-ctx.Done():
				env.Logger.Info("builder dispatch daemon stopped")
				return
			case <-ticker.C:
				processImplementingProposals(ctx, env)
			}
		}
	}()
}

func processImplementingProposals(ctx context.Context, env *runtimeEnv) {
	summaries, err := env.ProposalStore.List()
	if err != nil {
		env.Logger.Warn("builder dispatcher failed to list proposals", gozap.Error(err))
		return
	}

	for _, summary := range summaries {
		if _, loaded := builderDispatchInFlight.LoadOrStore(summary.ID, struct{}{}); loaded {
			continue
		}

		go func(proposalID string) {
			defer builderDispatchInFlight.Delete(proposalID)
			if buildErr := buildImplementingProposal(ctx, env, proposalID); buildErr != nil {
				env.Logger.Error("builder dispatcher failed", gozap.String("proposal_id", proposalID), gozap.Error(buildErr))
			}
		}(summary.ID)
	}
}

func buildImplementingProposal(ctx context.Context, env *runtimeEnv, proposalID string) error {
	prop, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		return fmt.Errorf("load proposal: %w", err)
	}
	if prop.Status == proposal.StatusApproved {
		if err := prop.Transition(proposal.StatusImplementing, "builder dispatcher queued approved proposal", "builder-dispatcher"); err != nil {
			return fmt.Errorf("transition approved proposal to implementing: %w", err)
		}
		if err := env.ProposalStore.Update(prop); err != nil {
			return fmt.Errorf("persist implementing proposal: %w", err)
		}
		env.Logger.Info("builder dispatcher queued approved proposal",
			gozap.String("proposal_id", prop.ID),
			gozap.String("skill", prop.TargetSkill),
		)
	}
	if prop.Status != proposal.StatusImplementing {
		return nil
	}

	spec, err := localSkillSpecFromProposal(prop)
	if err != nil {
		return markProposalFailed(env, prop, fmt.Sprintf("invalid skill spec: %v", err))
	}

	payload, _ := json.Marshal(map[string]string{"proposal_id": prop.ID, "skill": spec.Name})
	if _, logErr := env.Kernel.SignAndLog(kernel.NewAction(kernel.ActionBuilderStart, "builder-dispatcher", payload)); logErr != nil {
		env.Logger.Warn("failed to audit builder dispatch start", gozap.Error(logErr))
	}

	files, reasoning, err := executeBuildInMicroVM(ctx, env, prop, spec)
	if err != nil {
		return markProposalFailed(env, prop, fmt.Sprintf("microVM build failed: %v", err))
	}

	repoKind := gitmanager.RepoSkills
	if prop.Category == proposal.CategoryKernelPatch {
		repoKind = gitmanager.RepoSelf
	}

	if err := ensureProposalBranch(env, repoKind, prop.ID); err != nil {
		return markProposalFailed(env, prop, fmt.Sprintf("git branch setup failed: %v", err))
	}

	commitMsg := fmt.Sprintf("feat(%s): %s\n\nProposal: %s\nBuilt in AegisClaw builder microVM", spec.Name, prop.Title, prop.ID)
	commitHash, err := env.GitManager.CommitFiles(repoKind, files, commitMsg)
	if err != nil {
		return markProposalFailed(env, prop, fmt.Sprintf("git commit failed: %v", err))
	}

	diff, err := env.GitManager.GenerateDiff(repoKind, prop.ID)
	if err != nil {
		env.Logger.Warn("failed to generate diff", gozap.String("proposal_id", prop.ID), gozap.Error(err))
	}

	if err := prop.Transition(proposal.StatusComplete, "build completed successfully", "builder-dispatcher"); err != nil {
		return fmt.Errorf("transition complete: %w", err)
	}
	if err := env.ProposalStore.Update(prop); err != nil {
		return fmt.Errorf("persist completed proposal: %w", err)
	}

	result := &builder.PipelineResult{
		ProposalID:   prop.ID,
		State:        builder.PipelineStateComplete,
		BuilderID:    "builder-microvm",
		CommitHash:   commitHash,
		Branch:       "proposal-" + prop.ID,
		Diff:         diff,
		Files:        files,
		FileHashes:   map[string]string{},
		Reasoning:    reasoning,
		StartedAt:    time.Now().UTC(),
		CompletedAt:  time.Now().UTC(),
		Duration:     0,
	}
	createPRFromPipelineResult(env, prop.ID, result.Branch, commitHash, result)

	completePayload, _ := json.Marshal(map[string]string{"proposal_id": prop.ID, "commit": commitHash})
	if _, logErr := env.Kernel.SignAndLog(kernel.NewAction(kernel.ActionBuilderBuild, "builder-dispatcher", completePayload)); logErr != nil {
		env.Logger.Warn("failed to audit builder dispatch completion", gozap.Error(logErr))
	}

	env.Logger.Info("builder microVM completed proposal",
		gozap.String("proposal_id", prop.ID),
		gozap.String("skill", spec.Name),
		gozap.String("commit", truncateCommitHash(commitHash)),
	)
	return nil
}

func executeBuildInMicroVM(ctx context.Context, env *runtimeEnv, prop *proposal.Proposal, spec *builder.SkillSpec) (map[string]string, string, error) {
	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: env.Config.Firecracker.Bin,
		JailerBin:      env.Config.Jailer.Bin,
		KernelImage:    env.Config.Sandbox.KernelImage,
		RootfsTemplate: env.Config.Builder.RootfsTemplate,
		ChrootBaseDir:  env.Config.Sandbox.ChrootBase,
		StateDir:       env.Config.Sandbox.StateDir,
	}

	if env.LLMProxy == nil {
		allowedModels := llm.AllowedModelsFromRegistry()
		env.LLMProxy = llm.NewOllamaProxyWithHTTPClient(allowedModels, "", env.OllamaHTTPClient, env.Kernel, env.Logger)
	}

	launcher := builder.NewFirecrackerBuilderLauncher(env.Runtime, rtCfg, env.LLMProxy, env.Logger)
	builderID, err := launcher.LaunchBuilder(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if stopErr := launcher.StopBuilder(context.Background(), builderID); stopErr != nil {
			env.Logger.Warn("failed to stop builder sandbox", gozap.String("builder_id", builderID), gozap.Error(stopErr))
		}
	}()

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, "", fmt.Errorf("marshal skill spec: %w", err)
	}

	resp, err := launcher.SendBuildRequest(ctx, builderID, &builder.BuildRequest{
		ProposalID:  prop.ID,
		Title:       prop.Title,
		Description: prop.Description,
		Spec:        specJSON,
		Round:       prop.Round,
	})
	if err != nil {
		return nil, "", err
	}
	if resp.State != builder.PipelineStateComplete {
		return nil, "", fmt.Errorf("builder returned state=%s error=%s", resp.State, resp.Error)
	}
	return resp.Files, resp.Reasoning, nil
}

func markProposalFailed(env *runtimeEnv, prop *proposal.Proposal, reason string) error {
	if err := prop.Transition(proposal.StatusFailed, reason, "builder-dispatcher"); err != nil {
		return err
	}
	return env.ProposalStore.Update(prop)
}

func ensureProposalBranch(env *runtimeEnv, kind gitmanager.RepoKind, proposalID string) error {
	if err := env.GitManager.CreateProposalBranch(kind, proposalID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "exists") || strings.Contains(strings.ToLower(err.Error()), "reference") {
			return env.GitManager.CheckoutProposalBranch(kind, proposalID)
		}
		return err
	}
	return nil
}

func localSkillSpecFromProposal(prop *proposal.Proposal) (*builder.SkillSpec, error) {
	if len(prop.Spec) == 0 {
		return nil, fmt.Errorf("proposal spec is required for builder microVM execution")
	}

	var spec builder.SkillSpec
	if err := json.Unmarshal(prop.Spec, &spec); err != nil {
		return nil, err
	}
	if spec.Language == "" {
		spec.Language = "go"
	}
	if len(spec.Tools) == 0 {
		spec.Tools = []builder.ToolSpec{{
			Name:         "run",
			Description:  "Execute the generated skill",
			InputSchema:  "{}",
			OutputSchema: "{}",
		}}
	}
	if !spec.NetworkPolicy.DefaultDeny {
		spec.NetworkPolicy.DefaultDeny = true
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}