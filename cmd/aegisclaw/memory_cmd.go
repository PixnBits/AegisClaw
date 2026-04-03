package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/spf13/cobra"
)

// memoryCmd is the top-level `aegisclaw memory` command group.
var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage the persistent Memory Store (Phase 1)",
	Long: `Commands for querying and managing the AegisClaw tiered Memory Store.

The Memory Store provides persistent, encrypted storage for agent memories
across sessions.  All operations are audited in the Merkle tree.

  memory search  Search memories by keyword or semantic query
  memory list    List stored memories (optionally filtered by TTL tier)
  memory compact Trigger tier-based compaction to reduce storage usage
  memory delete  Soft-delete memories matching a query (GDPR right-to-forget)
`,
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search memories by keyword",
	Long:  `Retrieves stored memories matching the given keyword query.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMemorySearch,
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored memories",
	Long:  `Lists all non-deleted memories, optionally filtered by TTL tier (90d, 180d, 365d, 2yr, forever).`,
	RunE:  runMemoryList,
}

var memoryCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Compact memories (tier transition)",
	Long:  `Triggers the memory compaction daemon to transition entries to coarser tiers to reduce storage.`,
	RunE:  runMemoryCompact,
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <query>",
	Short: "Delete (soft-delete) memories matching a query",
	Long:  `Soft-deletes all memory entries matching the given query. This is the GDPR right-to-forget operation.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runMemoryDelete,
}

var memoryTierFlag string
var memoryKFlag int
var memoryTaskIDFlag string

func init() {
	memorySearchCmd.Flags().IntVarP(&memoryKFlag, "k", "k", 5, "Maximum number of results to return")
	memorySearchCmd.Flags().StringVar(&memoryTaskIDFlag, "task-id", "", "Filter by task ID")
	memoryListCmd.Flags().StringVar(&memoryTierFlag, "tier", "", "Filter by TTL tier (90d, 180d, 365d, 2yr, forever)")
	memoryCompactCmd.Flags().StringVar(&memoryTierFlag, "tier", "", "Target TTL tier for compaction (default: all eligible)")
	memoryCompactCmd.Flags().StringVar(&memoryTaskIDFlag, "task-id", "", "Compact only entries for this task ID")
}

// callMemoryTool is a helper that routes a tool call through the daemon's
// chat.tool endpoint and prints the output.
func callMemoryTool(ctx context.Context, env *runtimeEnv, tool string, args interface{}) error {
	client := api.NewClient(env.Config.Daemon.SocketPath)
	reqArgs, _ := json.Marshal(args)
	resp, err := client.Call(ctx, "chat.tool", api.ChatToolExecRequest{
		Name: tool,
		Args: string(reqArgs),
	})
	if err != nil {
		return fmt.Errorf("daemon unavailable (is `aegisclaw start` running?): %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("%s: %s", tool, resp.Error)
	}
	var result struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil || result.Output == "" {
		fmt.Printf("%s\n", strings.TrimSpace(string(resp.Data)))
		return nil
	}
	fmt.Println(result.Output)
	return nil
}

func runMemorySearch(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()
	return callMemoryTool(cmd.Context(), env, "retrieve_memory", map[string]interface{}{
		"query":   strings.Join(args, " "),
		"k":       memoryKFlag,
		"task_id": memoryTaskIDFlag,
	})
}

func runMemoryList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()
	return callMemoryTool(cmd.Context(), env, "list_memories", map[string]string{
		"tier": memoryTierFlag,
	})
}

func runMemoryCompact(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()
	return callMemoryTool(cmd.Context(), env, "compact_memory", map[string]string{
		"task_id":     memoryTaskIDFlag,
		"target_tier": memoryTierFlag,
	})
}

func runMemoryDelete(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()
	query := strings.Join(args, " ")
	if !globalForce {
		fmt.Printf("This will soft-delete all memories matching %q.\n", query)
		fmt.Println("Use --force to confirm.")
		return nil
	}
	return callMemoryTool(cmd.Context(), env, "delete_memory", map[string]string{
		"query": query,
	})
}