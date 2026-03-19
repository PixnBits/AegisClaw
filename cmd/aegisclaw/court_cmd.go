package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/spf13/cobra"
)

var courtCmd = &cobra.Command{
	Use:   "court",
	Short: "Governance court operations",
	Long: `Commands for managing the multi-persona court review process.
The court reviews proposals using AI personas, each running in isolated sandboxes.`,
}

var courtReviewCmd = &cobra.Command{
	Use:   "review <proposal-id>",
	Short: "Start or continue a court review session",
	Long: `Runs the full court review process for a proposal.
Each persona reviews independently in its own Firecracker sandbox.
Multiple rounds of review may occur until consensus is reached.`,
	Args: cobra.ExactArgs(1),
	RunE: runCourtReview,
}

var courtVoteCmd = &cobra.Command{
	Use:   "vote <proposal-id> <approve|reject> <reason>",
	Short: "Cast a human vote on an escalated proposal",
	Long: `Provides a human override for proposals that could not reach
automated consensus. This is the Enterprise-mode manual review mechanism.`,
	Args: cobra.ExactArgs(3),
	RunE: runCourtVote,
}

var courtSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List active court sessions",
	Long:  `Displays all non-finalized court review sessions.`,
	RunE:  runCourtSessions,
}

func initCourtEngine(env *runtimeEnv) (*court.Engine, error) {
	personaDir := env.Config.Court.PersonaDir
	if personaDir == "" {
		var err error
		personaDir, err = court.DefaultPersonaDir()
		if err != nil {
			return nil, fmt.Errorf("failed to determine default persona dir: %w", err)
		}
	}

	personas, err := court.LoadPersonas(personaDir, env.Logger)
	if err != nil {
		// Try to create defaults if dir doesn't exist
		var createDir string
		createDir, err = court.EnsureDefaultPersonas(env.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create default personas: %w", err)
		}
		personaDir = createDir
		personas, err = court.LoadPersonas(personaDir, env.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load personas after creating defaults: %w", err)
		}
	}

	launcher := court.NewFirecrackerLauncher(
		env.Runtime,
		env.Kernel,
		sandbox.RuntimeConfig{
			FirecrackerBin: env.Config.Firecracker.Bin,
			JailerBin:      env.Config.Jailer.Bin,
			KernelImage:    env.Config.Sandbox.KernelImage,
			RootfsTemplate: env.Config.Rootfs.Template,
			ChrootBaseDir:  env.Config.Sandbox.ChrootBase,
			StateDir:       env.Config.Sandbox.StateDir,
		},
		env.Logger,
	)
	reviewer := court.NewReviewer(launcher, 2, env.Logger)
	reviewerFn := court.NewReviewerFunc(reviewer)

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, env.ProposalStore, env.Kernel, personas, reviewerFn, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create court engine: %w", err)
	}

	return engine, nil
}

func runCourtReview(cmd *cobra.Command, args []string) error {
	proposalID := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	engine, err := initCourtEngine(env)
	if err != nil {
		return err
	}

	fmt.Printf("Starting court review for proposal %s...\n\n", proposalID)

	session, err := engine.Review(context.Background(), proposalID)
	if err != nil {
		return fmt.Errorf("court review failed: %w", err)
	}

	printSessionSummary(session)
	return nil
}

func runCourtVote(cmd *cobra.Command, args []string) error {
	proposalID := args[0]
	voteStr := strings.ToLower(args[1])
	reason := args[2]

	var approve bool
	switch voteStr {
	case "approve", "yes", "y":
		approve = true
	case "reject", "no", "n":
		approve = false
	default:
		return fmt.Errorf("vote must be 'approve' or 'reject', got %q", voteStr)
	}

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	engine, err := initCourtEngine(env)
	if err != nil {
		return err
	}

	session, err := engine.VoteOnProposal(context.Background(), proposalID, "operator", approve, reason)
	if err != nil {
		return fmt.Errorf("vote failed: %w", err)
	}

	fmt.Printf("Vote recorded.\n\n")
	printSessionSummary(session)
	return nil
}

func runCourtSessions(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	engine, err := initCourtEngine(env)
	if err != nil {
		return err
	}

	sessions := engine.ActiveSessions()
	if len(sessions) == 0 {
		fmt.Println("No active court sessions.")
		return nil
	}

	fmt.Printf("%-38s %-38s %-12s %-6s %-8s\n",
		"SESSION ID", "PROPOSAL ID", "STATE", "ROUND", "RISK")
	fmt.Println(strings.Repeat("-", 106))

	for _, s := range sessions {
		fmt.Printf("%-38s %-38s %-12s %-6d %-8.1f\n",
			s.ID, s.ProposalID, s.State, s.Round, s.RiskScore)
	}
	return nil
}

func printSessionSummary(session *court.Session) {
	fmt.Printf("Court Session: %s\n", session.ID)
	fmt.Printf("  Proposal: %s\n", session.ProposalID)
	fmt.Printf("  State:    %s\n", session.State)
	fmt.Printf("  Verdict:  %s\n", session.Verdict)
	fmt.Printf("  Risk:     %.1f\n", session.RiskScore)
	fmt.Printf("  Rounds:   %d\n", session.Round)

	if len(session.Results) > 0 {
		fmt.Printf("\nRound Results:\n")
		for _, result := range session.Results {
			fmt.Printf("  Round %d (consensus=%v, avg_risk=%.1f):\n",
				result.Round, result.Consensus, result.AvgRisk)

			if len(result.Reviews) > 0 {
				fmt.Printf("    %-15s %-10s %-6s %s\n", "PERSONA", "VERDICT", "RISK", "COMMENTS")
				fmt.Printf("    %s\n", strings.Repeat("-", 60))
				for _, r := range result.Reviews {
					comments := r.Comments
					if len(comments) > 25 {
						comments = comments[:25] + ".."
					}
					fmt.Printf("    %-15s %-10s %-6.1f %s\n",
						r.Persona, r.Verdict, r.RiskScore, comments)
				}
			}

			if result.Feedback != nil && result.Feedback.HasQuestions {
				fmt.Printf("    Questions for next round:\n")
				for _, q := range result.Feedback.Questions {
					fmt.Printf("      - %s\n", q)
				}
			}

			if len(result.Heatmap) > 0 {
				fmt.Printf("    Risk Heatmap: ")
				parts := make([]string, 0, len(result.Heatmap))
				for persona, risk := range result.Heatmap {
					parts = append(parts, fmt.Sprintf("%s=%.1f", persona, risk))
				}
				fmt.Println(strings.Join(parts, ", "))
			}
		}
	}

	if session.State == court.SessionEscalated {
		fmt.Printf("\n⚠ Proposal escalated: requires human vote.\n")
		fmt.Printf("  aegisclaw court vote %s approve \"reason\"\n", session.ProposalID)
		fmt.Printf("  aegisclaw court vote %s reject \"reason\"\n", session.ProposalID)
	}
}
