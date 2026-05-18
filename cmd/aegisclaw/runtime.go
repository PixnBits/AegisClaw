package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/aegishub"
	"go.uber.org/zap"
)

// AegisHubMonitor holds lifecycle state for AegisHub.
type AegisHubMonitor struct {
	client  aegishub.Client
	logger  *zap.Logger
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// launchAegisHub now returns a monitor with cancellable health checking.
func launchAegisHub(logger *zap.Logger) (*AegisHubMonitor, error) {
	logger.Info("Phase 3.5: Launching AegisHub with hardened monitoring")

	client, err := aegishub.NewClient("")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to AegisHub: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	monitor := &AegisHubMonitor{
		client: client,
		logger: logger,
		cancel: cancel,
	}

	monitor.wg.Add(1)
	go monitor.healthLoop(ctx)

	logger.Info("AegisHub launched with active monitoring")
	return monitor, nil
}

func (m *AegisHubMonitor) healthLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("AegisHub health monitoring stopped")
			return
		case <-ticker.C:
			m.logger.Debug("AegisHub health check")
			// TODO: Implement real health check (e.g. vsock ping to AegisHub)
		}
	}
}

// Stop gracefully shuts down monitoring and closes the client.
func (m *AegisHubMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	if m.client != nil {
		m.client.Close()
	}
	m.logger.Info("AegisHub monitor stopped cleanly")
}

// shutdownAegisHub is kept for backward compatibility.
func shutdownAegisHub(monitor *AegisHubMonitor, logger *zap.Logger) {
	if monitor != nil {
		monitor.Stop()
	}
}
