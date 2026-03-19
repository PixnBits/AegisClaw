package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var courtDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Interactive court review dashboard",
	Long: `Opens a full-screen interactive TUI for reviewing proposals.
Browse proposals, view per-persona reviews, inspect diffs, and cast votes.`,
	RunE: runCourtDashboard,
}

func runCourtDashboard(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	engine, err := initCourtEngine(env)
	if err != nil {
		return err
	}

	model := tui.NewCourtDashboard()

	model.LoadProposals = func() ([]tui.CourtProposal, error) {
		summaries, err := env.ProposalStore.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list proposals: %w", err)
		}
		result := make([]tui.CourtProposal, len(summaries))
		for i, s := range summaries {
			result[i] = tui.CourtProposal{
				ID:       s.ID,
				Title:    s.Title,
				Category: string(s.Category),
				Status:   string(s.Status),
				Risk:     string(s.Risk),
				Author:   s.Author,
				Round:    s.Round,
				Updated:  s.UpdatedAt,
			}
		}
		return result, nil
	}

	model.LoadSessions = func() ([]tui.CourtSession, error) {
		sessions := engine.ActiveSessions()
		result := make([]tui.CourtSession, len(sessions))
		for i, s := range sessions {
			cs := tui.CourtSession{
				SessionID:  s.ID,
				ProposalID: s.ProposalID,
				State:      string(s.State),
				Round:      s.Round,
				RiskScore:  s.RiskScore,
				Verdict:    s.Verdict,
				Personas:   s.Personas,
			}
			for _, rr := range s.Results {
				for _, r := range rr.Reviews {
					cs.Reviews = append(cs.Reviews, tui.CourtReview{
						Persona:   r.Persona,
						Verdict:   string(r.Verdict),
						RiskScore: r.RiskScore,
						Comments:  r.Comments,
						Evidence:  r.Evidence,
						Round:     r.Round,
					})
				}
			}
			result[i] = cs
		}
		return result, nil
	}

	model.LoadDiff = func(proposalID string) (string, error) {
		p, err := env.ProposalStore.Get(proposalID)
		if err != nil {
			return "", fmt.Errorf("failed to load proposal: %w", err)
		}
		if p.Spec != nil {
			return string(p.Spec), nil
		}
		return "(no spec/diff available)", nil
	}

	model.CastVote = func(proposalID string, approve bool, reason string) error {
		actor := "operator"
		_, err := engine.VoteOnProposal(context.Background(), proposalID, actor, approve, reason)
		if err != nil {
			return fmt.Errorf("vote failed: %w", err)
		}
		return nil
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
