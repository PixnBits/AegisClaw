package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/spf13/cobra"
)

// workerCmd is the top-level `aegisclaw worker` command group.
var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Inspect and manage ephemeral Worker agents",
	Long: `Commands for inspecting Worker agents spawned by the Orchestrator.

  worker list     List recent workers (active and completed)
  worker status   Get detailed status and result for a worker
`,
}

var workerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent worker records",
	RunE:  runWorkerList,
}

var workerStatusCmd = &cobra.Command{
	Use:   "status <worker-id>",
	Short: "Get detailed status for a worker",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkerStatus,
}

var workerActiveFlag bool

func init() {
	workerListCmd.Flags().BoolVar(&workerActiveFlag, "active", false, "Show only active workers")
}

func runWorkerList(cmd *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	data, err := callWorkerAPI(cmd.Context(), env, "worker.list", map[string]bool{
		"active_only": workerActiveFlag,
	})
	if err != nil {
		return err
	}

	var workers []*worker.WorkerRecord
	if err := json.Unmarshal(data, &workers); err != nil {
		fmt.Println(string(data))
		return nil
	}

	if len(workers) == 0 {
		if workerActiveFlag {
			fmt.Println("No active workers.")
		} else {
			fmt.Println("No worker records found.")
		}
		return nil
	}

	fmt.Printf("Workers (%d):\n", len(workers))
	for _, w := range workers {
		dur := ""
		if w.FinishedAt != nil {
			dur = fmt.Sprintf("  %s", w.FinishedAt.Sub(w.SpawnedAt).Round(time.Second))
		}
		fmt.Printf("  [%s]  %-11s  %-12s  steps=%-3d  %s%s\n",
			w.WorkerID[:8], w.Status, w.Role, w.StepCount,
			w.SpawnedAt.Format("2006-01-02T15:04"), dur)
	}
	return nil
}

func runWorkerStatus(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	data, err := callWorkerAPI(cmd.Context(), env, "worker.status", map[string]string{
		"worker_id": args[0],
	})
	if err != nil {
		return err
	}

	var w worker.WorkerRecord
	if err := json.Unmarshal(data, &w); err != nil {
		fmt.Println(string(data))
		return nil
	}
	fmt.Print(formatWorkerRecord(&w))
	return nil
}

// callWorkerAPI is a helper for the worker CLI commands.
func callWorkerAPI(ctx context.Context, env *runtimeEnv, action string, req interface{}) (json.RawMessage, error) {
	client := api.NewClient(env.Config.Daemon.SocketPath)
	var reqData json.RawMessage
	if req != nil {
		var err error
		reqData, err = json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}
	resp, err := client.Call(ctx, action, reqData)
	if err != nil {
		return nil, fmt.Errorf("daemon unavailable (is `aegisclaw start` running?): %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s: %s", action, resp.Error)
	}
	return resp.Data, nil
}
