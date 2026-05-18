package main

import (
	"context"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/store"
	"go.uber.org/zap"
)

// ToolRegistryClient is the Phase 3.2 seam.
// The Host Daemon should eventually only hold a thin client to the
// authoritative Tool Registry served by AegisHub.
type ToolRegistryClient interface {
	ListTools(ctx context.Context) ([]string, error)
	// Add more methods as AegisHub implements them
}

// stubToolRegistryClient is a temporary in-process implementation.
// Will be replaced by a real client that talks to AegisHub.
type stubToolRegistryClient struct{}

func (s *stubToolRegistryClient) ListTools(ctx context.Context) ([]string, error) {
	return []string{"daemon.ping", "search_tools (via AegisHub)"}, nil
}

// launchStoreVM and other functions remain from previous work...
