package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var proposeCategory string

var proposeCmd = &cobra.Command{
	Use:   "propose <title> [description]",
	Short: "Create a new governance proposal",
	Long: `Creates a new governance proposal for review by the court.
The proposal starts in draft status and must be submitted for review.
Use --category to specify the proposal type (default: new_skill).

Examples:
  aegisclaw propose "Add Redis skill" "Install and configure a Redis caching layer"
  aegisclaw propose "Security patch" "Fix CVE-2024-XXXX" --category kernel_patch`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPropose,
}

var proposeSubmitCmd = &cobra.Command{
	Use:   "submit <proposal-id>",
	Short: "Submit a draft proposal for court review",
	Long:  `Transitions a draft proposal to submitted status, making it eligible for court review.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runProposeSubmit,
}

var proposeListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all proposals",
	Long:  `Displays a table of all proposals with their status, risk, and review summary.`,
	RunE:  runProposeList,
}

var proposeShowCmd = &cobra.Command{
	Use:   "show <proposal-id>",
	Short: "Show proposal details",
	Long:  `Displays full details of a proposal including reviews, risk scores, and history.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runProposeShow,
}

func runPropose(cmd *cobra.Command, args []string) error {
	title := args[0]
	description := title
	if len(args) > 1 {
		description = args[1]
	}

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	cat := proposal.Category(proposeCategory)
	p, err := proposal.NewProposal(title, description, cat, "operator")
	if err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	if err := env.ProposalStore.Create(p); err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID,
		"title":       p.Title,
		"category":    string(p.Category),
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "operator", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log proposal creation", zap.Error(signErr))
	}

	fmt.Printf("Proposal created.\n")
	fmt.Printf("  ID:       %s\n", p.ID)
	fmt.Printf("  Title:    %s\n", p.Title)
	fmt.Printf("  Category: %s\n", p.Category)
	fmt.Printf("  Status:   %s\n", p.Status)
	fmt.Printf("\nSubmit for review: aegisclaw propose submit %s\n", p.ID)
	return nil
}

func runProposeSubmit(cmd *cobra.Command, args []string) error {
	proposalID := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	p, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		return fmt.Errorf("proposal not found: %w", err)
	}

	if err := p.Transition(proposal.StatusSubmitted, "submitted for review", "operator"); err != nil {
		return fmt.Errorf("cannot submit: %w", err)
	}

	if err := env.ProposalStore.Update(p); err != nil {
		return fmt.Errorf("failed to persist: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"proposal_id": proposalID})
	action := kernel.NewAction(kernel.ActionProposalSubmit, "operator", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log proposal submission", zap.Error(signErr))
	}

	fmt.Printf("Proposal %s submitted for court review.\n", p.ID)
	fmt.Printf("  Status: %s\n", p.Status)
	fmt.Printf("\nStart review: aegisclaw court review %s\n", p.ID)
	return nil
}

func runProposeList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	proposals, err := env.ProposalStore.List()
	if err != nil {
		return fmt.Errorf("failed to list proposals: %w", err)
	}

	if len(proposals) == 0 {
		fmt.Println("No proposals found.")
		return nil
	}

	// TUI summary table
	fmt.Printf("%-38s %-30s %-12s %-8s %-6s\n",
		"ID", "TITLE", "STATUS", "RISK", "ROUND")
	fmt.Println(strings.Repeat("-", 98))

	for _, p := range proposals {
		title := p.Title
		if len(title) > 28 {
			title = title[:28] + ".."
		}
		fmt.Printf("%-38s %-30s %-12s %-8s %-6d\n",
			p.ID, title, p.Status, p.Risk, p.Round)
	}
	return nil
}

func runProposeShow(cmd *cobra.Command, args []string) error {
	proposalID := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	p, err := env.ProposalStore.Get(proposalID)
	if err != nil {
		return fmt.Errorf("proposal not found: %w", err)
	}

	fmt.Printf("Proposal: %s\n", p.ID)
	fmt.Printf("  Title:       %s\n", p.Title)
	fmt.Printf("  Description: %s\n", p.Description)
	fmt.Printf("  Category:    %s\n", p.Category)
	fmt.Printf("  Status:      %s\n", p.Status)
	fmt.Printf("  Risk:        %s\n", p.Risk)
	fmt.Printf("  Author:      %s\n", p.Author)
	fmt.Printf("  Round:       %d\n", p.Round)
	fmt.Printf("  Version:     %d\n", p.Version)
	fmt.Printf("  Created:     %s\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Updated:     %s\n", p.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Hash:        %s\n", p.MerkleHash[:16])

	if len(p.Reviews) > 0 {
		fmt.Printf("\nReviews (%d):\n", len(p.Reviews))
		fmt.Printf("  %-15s %-10s %-8s %-6s %s\n", "PERSONA", "VERDICT", "RISK", "ROUND", "COMMENTS")
		fmt.Println("  " + strings.Repeat("-", 70))
		for _, r := range p.Reviews {
			comments := r.Comments
			if len(comments) > 30 {
				comments = comments[:30] + ".."
			}
			fmt.Printf("  %-15s %-10s %-8.1f %-6d %s\n",
				r.Persona, r.Verdict, r.RiskScore, r.Round, comments)
		}
	}

	if len(p.History) > 0 {
		fmt.Printf("\nHistory (%d transitions):\n", len(p.History))
		for _, h := range p.History {
			fmt.Printf("  %s -> %s  by %s  (%s)\n",
				h.From, h.To, h.Actor, h.Reason)
		}
	}

	return nil
}
