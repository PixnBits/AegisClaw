package main

import (
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var selfCmd = &cobra.Command{
	Use:   "self",
	Short: "Self-improvement and system management proposals",
	Long:  `Commands for proposing and tracking system self-improvement changes.`,
}

var selfProposeCmd = &cobra.Command{
	Use:   "propose <description>",
	Short: "Start a Court-reviewed self-improvement proposal",
	Long: `Creates a proposal for system self-improvement and submits it
for Governance Court review. This is how the system evolves itself.`,
	Args: cobra.ExactArgs(1),
	RunE: runSelfPropose,
}

var selfStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show self-improvement proposal status",
	Long:  `Displays the status of active self-improvement proposals.`,
	RunE:  runSelfStatus,
}

func init() {
	selfCmd.AddCommand(selfProposeCmd)
	selfCmd.AddCommand(selfStatusCmd)
}

func runSelfPropose(cmd *cobra.Command, args []string) error {
	description := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	p, err := proposal.NewProposal(
		"Self-improvement: "+description,
		description,
		proposal.CategoryKernelPatch,
		"system",
	)
	if err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	if err := env.ProposalStore.Create(p); err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	// Auto-submit for court review.
	if err := p.Transition(proposal.StatusSubmitted, "submitted for review", "system"); err != nil {
		return fmt.Errorf("cannot submit: %w", err)
	}
	if err := env.ProposalStore.Update(p); err != nil {
		return fmt.Errorf("failed to persist: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID,
		"title":       p.Title,
		"category":    string(p.Category),
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "system", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log self-improvement proposal", zap.Error(signErr))
	}

	fmt.Printf("Self-improvement proposal created and submitted.\n")
	fmt.Printf("  ID:       %s\n", p.ID)
	fmt.Printf("  Title:    %s\n", p.Title)
	fmt.Printf("  Status:   %s\n", p.Status)

	return nil
}

func runSelfStatus(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	proposals, err := env.ProposalStore.List()
	if err != nil {
		return fmt.Errorf("failed to list proposals: %w", err)
	}

	found := false
	for _, p := range proposals {
		if p.Category == proposal.CategoryKernelPatch {
			if !found {
				fmt.Println("Self-improvement proposals:")
				found = true
			}
			fmt.Printf("  %s  %-12s  %s\n", p.ID[:8], p.Status, p.Title)
		}
	}
	if !found {
		fmt.Println("No self-improvement proposals found.")
	}
	return nil
}
