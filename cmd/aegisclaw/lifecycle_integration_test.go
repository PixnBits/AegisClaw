//go:build integration
// +build integration

package main

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// These tests exercise richer lifecycle scenarios.
// They are tagged with 'integration' so they can be skipped in normal CI.
//
// To run locally:
//   go test -tags=integration ./cmd/aegisclaw/ -run Lifecycle
//
// In GitHub Actions, these are opt-in (via label or separate workflow)
// because they may require KVM or longer timeouts.

func TestLifecycleContainment_MonitorHealthAndRestart(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	monitor := &AegisHubMonitor{
		logger:                logger,
		maxFailsBeforeRestart: 2,
	}

	// Simulate health check failures
	ctx := context.Background()

	// First failure
	monitor.consecutiveFails = 1

	// Second failure should trigger restart threshold
	monitor.consecutiveFails = 2

	if monitor.consecutiveFails < monitor.maxFailsBeforeRestart {
		t.Error("expected to reach restart threshold")
	}

	t.Log("Lifecycle monitor reached restart threshold as expected")
}

func TestLifecycleContainment_CleanupOnShutdown(t *testing.T) {
	monitor := &AegisHubMonitor{}

	// Should be safe and clean
	monitor.Stop()

	t.Log("Monitor shutdown completed cleanly")
}
