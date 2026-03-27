package main

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/spf13/cobra"
)

var stopDaemonCmd = &cobra.Command{
	Use:   "stop",
	Short: "Gracefully stop the daemon",
	Long: `Gracefully shuts down all microVMs and the coordinator daemon.
Always logs the shutdown event to the audit trail.`,
	RunE: runStopDaemon,
}

func runStopDaemon(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "kernel.shutdown", nil)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w\n(Is the daemon running?)", err)
	}
	if !resp.Success {
		return fmt.Errorf("shutdown failed: %s", resp.Error)
	}

	fmt.Println("AegisClaw daemon shutdown initiated.")
	return nil
}
