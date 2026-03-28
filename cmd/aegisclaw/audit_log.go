package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/spf13/cobra"
)

var (
	auditLogSince string
	auditLogSkill string
	auditLogLimit int
)

var auditLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Query the append-only audit log",
	Long: `Displays entries from the Merkle-tree audit log with optional filtering.

Examples:
  aegisclaw audit log --since 24h
  aegisclaw audit log --skill my-skill --limit 10
  aegisclaw audit log --json`,
	RunE: runAuditLog,
}

var auditWhyCmd = &cobra.Command{
	Use:   "why <action-id>",
	Short: "Explain why an action was performed",
	Long: `Looks up the specified action by ID or hash prefix in the audit log
and displays its fields: hash, timestamp, actor, proposal, skill, and
full payload. Also verifies the entry's individual chain integrity.`,
	Args: cobra.ExactArgs(1),
	RunE: runAuditWhy,
}

func init() {
	auditLogCmd.Flags().StringVar(&auditLogSince, "since", "", "Show entries since time (e.g., 24h, 2024-01-01)")
	auditLogCmd.Flags().StringVar(&auditLogSkill, "skill", "", "Filter by skill ID")
	auditLogCmd.Flags().IntVar(&auditLogLimit, "limit", 50, "Maximum number of entries to show")
}

func runAuditLog(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	auditPath := filepath.Join(env.Config.Audit.Dir, "kernel.merkle.jsonl")

	entries, err := audit.ReadEntries(auditPath)
	if err != nil {
		return fmt.Errorf("failed to read audit log: %w", err)
	}

	// Apply filters.
	filtered := entries

	if auditLogSince != "" {
		var sinceTime time.Time
		// Try duration format first (e.g., "24h").
		if d, dErr := time.ParseDuration(auditLogSince); dErr == nil {
			sinceTime = time.Now().Add(-d)
		} else {
			// Try RFC3339 format.
			sinceTime, err = time.Parse(time.RFC3339, auditLogSince)
			if err != nil {
				// Try date-only format.
				sinceTime, err = time.Parse("2006-01-02", auditLogSince)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: use a duration (24h) or date (2024-01-01)", auditLogSince)
				}
			}
		}
		var timeFiltered []audit.MerkleEntry
		for _, e := range filtered {
			if e.Timestamp.After(sinceTime) {
				timeFiltered = append(timeFiltered, e)
			}
		}
		filtered = timeFiltered
	}

	if auditLogSkill != "" {
		var skillFiltered []audit.MerkleEntry
		for _, e := range filtered {
			if strings.Contains(string(e.Payload), auditLogSkill) {
				skillFiltered = append(skillFiltered, e)
			}
		}
		filtered = skillFiltered
	}

	// Apply limit (show most recent).
	if auditLogLimit > 0 && len(filtered) > auditLogLimit {
		filtered = filtered[len(filtered)-auditLogLimit:]
	}

	if len(filtered) == 0 {
		fmt.Println("No audit entries found matching criteria.")
		return nil
	}

	if globalJSON {
		data, _ := json.MarshalIndent(filtered, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Audit Log (%d entries):\n\n", len(filtered))
	for _, e := range filtered {
		ts := e.Timestamp.Format("2006-01-02 15:04:05")
		hash := e.Hash
		if len(hash) > 16 {
			hash = hash[:16]
		}
		payloadStr := string(e.Payload)
		if len(payloadStr) > 80 {
			payloadStr = payloadStr[:80] + "..."
		}
		fmt.Printf("  %s  %s  %s\n", ts, hash, payloadStr)
	}

	return nil
}

func runAuditWhy(cmd *cobra.Command, args []string) error {
	actionID := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	auditPath := filepath.Join(env.Config.Audit.Dir, "kernel.merkle.jsonl")

	entries, err := audit.ReadEntries(auditPath)
	if err != nil {
		return fmt.Errorf("failed to read audit log: %w", err)
	}

	// Find the target entry by ID or hash prefix.
	var target *audit.MerkleEntry
	for i, e := range entries {
		if e.ID == actionID || strings.HasPrefix(e.Hash, actionID) {
			target = &entries[i]
			break
		}
	}

	if target == nil {
		return fmt.Errorf("action %q not found in audit log", actionID)
	}

	if globalJSON {
		data, _ := json.MarshalIndent(target, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Action: %s\n", target.ID)
	fmt.Printf("  Hash:      %s\n", target.Hash)
	fmt.Printf("  Prev Hash: %s\n", target.PrevHash)
	fmt.Printf("  Timestamp: %s\n", target.Timestamp.Format(time.RFC3339))

	// Parse payload to extract action details.
	var payload map[string]interface{}
	if json.Unmarshal(target.Payload, &payload) == nil {
		if action, ok := payload["action"].(string); ok {
			fmt.Printf("  Action:    %s\n", action)
		}
		if actor, ok := payload["actor"].(string); ok {
			fmt.Printf("  Actor:     %s\n", actor)
		}
		if pid, ok := payload["proposal_id"].(string); ok {
			fmt.Printf("  Proposal:  %s\n", pid)
		}
		if skill, ok := payload["skill_name"].(string); ok {
			fmt.Printf("  Skill:     %s\n", skill)
		}
	}

	fmt.Printf("\n  Payload:\n    %s\n", string(target.Payload))

	// Check chain integrity for this entry.
	valid := target.Verify() == nil
	if valid {
		fmt.Printf("\n  Chain:     ✓ Entry verified\n")
	} else {
		fmt.Printf("\n  Chain:     ✗ Entry verification FAILED\n")
	}

	return nil
}
