package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

// setupLifecycleContainment registers signal handlers and ensures VMs are terminated
// when the daemon exits (normal shutdown or termination).
func setupLifecycleContainment(env *runtimeEnv, logger *zap.Logger) {
	 sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sigs
		logger.Warn("Received termination signal - initiating aggressive VM cleanup")

		// Stop AegisHub if running
		if env.AegisHubMonitor != nil {
			env.AegisHubMonitor.Stop()
		}

		// TODO: Add Store VM cleanup here when StoreVM monitor is available
		// if env.StoreVMMonitor != nil { env.StoreVMMonitor.Stop() }

		// Additional: best-effort cleanup of any remaining sandboxes
		if env.Runtime != nil {
			ctx := context.Background()
			_ = shutdownRuntimeSandboxes(ctx, env) // from earlier helper
		}

		logger.Info("Lifecycle containment cleanup complete")
		os.Exit(0)
	}()
}
