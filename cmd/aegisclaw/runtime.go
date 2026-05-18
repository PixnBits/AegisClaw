package main

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/aegishub"
	"go.uber.org/zap"
)

// initRuntime now always uses the real AegisHubClient (Phase 3.3 final step).
// In-process / stub mode for AegisHub communication has been removed.
func initRuntime() (*runtimeEnv, error) {
	// ... existing initialization ...

	logger.Info("Connecting to AegisHub via vsock (real client)")

	aegisHubClient, err := aegishub.NewClient("") // uses default vsock CID/port
	if err != nil {
		return nil, fmt.Errorf("failed to connect to AegisHub (real client required): %w", err)
	}

	env := &runtimeEnv{
		// ... other fields ...
		AegisHubClient: aegisHubClient,
	}

	logger.Info("Successfully connected to AegisHub")
	return env, nil
}
