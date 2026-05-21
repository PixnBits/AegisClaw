//go:build integration
// +build integration

package main

import (
	"testing"

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

	if monitor.OnHealthCheckFailed() {
		t.Fatal("first failed probe should not cross restart threshold")
	}
	if !monitor.OnHealthCheckFailed() {
		t.Fatal("second consecutive failure should cross restart threshold (DB-09)")
	}
	if !monitor.OnHealthCheckFailed() {
		t.Fatal("failures after threshold should keep reporting threshold crossed")
	}

	monitor.ResetHealthFailures()
	if monitor.OnHealthCheckFailed() {
		t.Fatal("after reset, first failure must not immediately cross threshold")
	}

	t.Log("Lifecycle monitor health failure accounting behaves as expected")
}

func TestLifecycleContainment_CleanupOnShutdown(t *testing.T) {
	monitor := &AegisHubMonitor{}

	// Should be safe and clean
	monitor.Stop()

	t.Log("Monitor shutdown completed cleanly")
}
