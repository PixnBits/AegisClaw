package main

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/config"
	aegispaths "github.com/PixnBits/AegisClaw/internal/paths"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var stopDaemonCmd = &cobra.Command{
	Use:   "stop",
	Short: "Gracefully stop the daemon",
	Long: `Gracefully shuts down all microVMs and the coordinator daemon.
Always logs the shutdown event to the audit trail.`,
	RunE: runStopDaemon,
}

func runStopDaemon(cmd *cobra.Command, args []string) error {
	client := api.NewClient(resolveDaemonSocketPath())
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

func resolveDaemonSocketPath() string {
	cfg, err := config.Load(zap.NewNop())
	if err != nil || cfg == nil || cfg.Daemon.SocketPath == "" {
		fallback, fallbackErr := aegispaths.DefaultSocketPath()
		if fallbackErr == nil {
			return fallback
		}
		return "/tmp/aegis-daemon.sock"
	}
	return cfg.Daemon.SocketPath
}
