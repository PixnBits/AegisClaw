package main

import (
	"context"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/runtime"
	"github.com/PixnBits/AegisClaw/internal/testutil"
	"github.com/PixnBits/AegisClaw/internal/workspace"
	"github.com/stretchr/testify/require"
)

func TestFullSDLC_SkillLifecycle_Issue35(t *testing.T) {
	ctx := testutil.NewTestContext(t)
	defer ctx.Cleanup()

	// === PHASE 1: Skill Proposal (Ideation + Workspace) ===
	t.Run("1_ProposalCreation", func(t *testing.T) {
		ws := workspace.New(ctx)
		skillName := "test-vision-analysis"
		proposalDesc := `Add a secure multimodal vision+audio perception skill using Nemotron-3-Nano-Omni.
			Required: read-only image/audio input, no network, no secrets.`

		edits := []workspace.Edit{
			{Path: "SOUL.md", Content: "# Vision Skill Proposal\n" + proposalDesc},
			{Path: ".SKILL.md", Content: `name: ` + skillName + `\npermissions: [read-image, read-audio]`},
		}
		p, err := proposal.CreateDraft(ctx, skillName, proposalDesc, edits)
		require.NoError(t, err)
		require.Equal(t, proposal.StatusDraft, p.Status)

		err = p.Submit(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, p.ProposalBranch)
	})

	// === PHASE 2: Governance Court Review (Pre-Implementation) ===
	t.Run("2_CourtProposalReview", func(t *testing.T) {
		c := court.NewCourt(ctx, court.WithPersonas(court.AllFive))
		review, err := c.ReviewProposal(ctx, proposal.CurrentProposal())
		require.NoError(t, err)
		require.True(t, review.ConsensusApproved, "Court must approve proposal")
		require.Greater(t, review.RiskScore, 0.0)
		require.NotEmpty(t, review.Evidence)
	})

	// === PHASE 3-4: Implementation + PR-Style Code Review ===
	t.Run("3_ImplementationAndPRReview", func(t *testing.T) {
		g := git.NewManager(ctx)

		buildResult, err := runtime.RunBuilderPipeline(ctx, proposal.CurrentProposal())
		require.NoError(t, err)
		require.True(t, buildResult.Success)

		pr, err := g.CreatePullRequest(ctx, buildResult.ProposalBranch, "feat: add vision-analysis skill")
		require.NoError(t, err)

		c := court.NewCourt(ctx, court.WithPersonas([]court.Persona{court.Coder, court.SecurityArchitect, court.Tester}))
		codeReview, err := c.ReviewPullRequest(ctx, pr)
		require.NoError(t, err)
		require.True(t, codeReview.Approved, "Court code review must pass")
		require.NotEmpty(t, codeReview.InlineComments)
	})

	// === PHASE 5: Security Gates ===
	t.Run("4_SecurityGates", func(t *testing.T) {
		gates := runtime.NewSecurityGates(ctx)
		results, err := gates.RunAll(ctx)
		require.NoError(t, err)
		require.True(t, results.AllPassed, "All mandatory gates (SAST/SCA/secrets/policy) must pass")
	})

	// === PHASE 6: Pre-Deployment Review + Approval Gate ===
	t.Run("5_DeploymentReview", func(t *testing.T) {
		preview := runtime.NewDeploymentPreview(ctx)
		previewData, err := preview.Generate(ctx)
		require.NoError(t, err)

		approval, err := court.NewCourt(ctx, court.WithPersonas([]court.Persona{court.CISO})).ApproveDeployment(ctx, previewData)
		require.NoError(t, err)
		require.True(t, approval.Approved)
	})

	// === PHASE 7: Deployment + Invocation ===
	t.Run("6_DeploymentAndInvocation", func(t *testing.T) {
		deployer := runtime.NewDeployer(ctx)
		sandboxID, err := deployer.Deploy(ctx, "test-vision-analysis")
		require.NoError(t, err)
		require.NotEmpty(t, sandboxID)

		result, err := runtime.InvokeSkill(ctx, sandboxID, "analyze_image", `{"image_base64": "testdata"}`)
		require.NoError(t, err)
		require.Contains(t, result.Output, "vision analysis complete")

		health := runtime.CheckHealth(ctx, sandboxID)
		require.True(t, health.Healthy)
	})

	// === FINAL VERIFICATIONS ===
	t.Run("7_AuditAndMerkle", func(t *testing.T) {
		audit.AssertTamperEvident(ctx, t)
		git.AssertSignedCommits(ctx, t)
	})
}