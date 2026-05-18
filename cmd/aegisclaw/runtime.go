package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/store"
	"go.uber.org/zap"
)

// ToolRegistryClient seam (Phase 3.2)
type ToolRegistryClient interface {
	ListTools(ctx context.Context) ([]string, error)
}

type stubToolRegistryClient struct{}

func (s *stubToolRegistryClient) ListTools(ctx context.Context) ([]string, error) {
	return []string{"daemon.ping", "search_tools (via AegisHub)"}, nil
}

// runtimeEnv now includes ToolRegistryClient
// Direct ToolRegistry usage should be deprecated in favor of this client.
type runtimeEnv struct {
	// ... existing fields ...
	ToolRegistryClient ToolRegistryClient
	// ...
}

func initRuntime() (*runtimeEnv, error) {
	// ... existing init code ...

	// Phase 3.2: Initialize ToolRegistryClient (points to AegisHub in future)
	env := &runtimeEnv{
		// ... other fields ...
		ToolRegistryClient: &stubToolRegistryClient{},
	}
	return env, nil
}
