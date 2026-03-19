package main

import (
	"fmt"
	"path/filepath"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var auditExplorerCmd = &cobra.Command{
	Use:   "explorer",
	Short: "Interactive audit log explorer",
	Long: `Opens a full-screen TUI for browsing the Merkle audit chain.
Search entries, inspect payloads, verify chain integrity, and initiate rollbacks.`,
	RunE: runAuditExplorer,
}

func runAuditExplorer(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	auditPath := filepath.Join(env.Config.Audit.Dir, "kernel.merkle.jsonl")

	model := tui.NewAuditExplorer()

	model.LoadEntries = func() ([]tui.AuditEntry, error) {
		entries, err := audit.ReadEntries(auditPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read audit log: %w", err)
		}
		result := make([]tui.AuditEntry, len(entries))
		for i, e := range entries {
			result[i] = tui.AuditEntry{
				ID:        e.ID,
				PrevHash:  e.PrevHash,
				Hash:      e.Hash,
				Timestamp: e.Timestamp,
				Payload:   string(e.Payload),
				Valid:     e.Verify() == nil,
			}
		}
		return result, nil
	}

	model.VerifyChain = func() (uint64, error) {
		return audit.VerifyChain(auditPath, env.Kernel.PublicKey())
	}

	model.RollbackEntry = func(entryID string) error {
		// Task 7.5 will implement full rollback; for now log intent
		env.Logger.Info("rollback requested via audit explorer")
		return fmt.Errorf("rollback engine not yet available (see Epic 7)")
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
