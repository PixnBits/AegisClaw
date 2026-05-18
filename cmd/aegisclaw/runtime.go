package main

// AegisHubMonitor now includes restart-on-failure logic.
type AegisHubMonitor struct {
	client            aegishub.Client
	logger            *zap.Logger
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	vm                interface{ Stop() error }
	consecutiveFails  int
	maxFailsBeforeRestart int
}

func NewAegisHubMonitor(...) { /* ... */ }

func (m *AegisHubMonitor) healthLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.client.Health(ctx); err != nil {
				m.consecutiveFails++
				m.logger.Warn("AegisHub health failed", zap.Int("consecutive", m.consecutiveFails), zap.Error(err))

				if m.consecutiveFails >= m.maxFailsBeforeRestart {
					m.logger.Error("AegisHub health degraded - attempting restart")
					m.restart()
					m.consecutiveFails = 0
				}
			} else {
				if m.consecutiveFails > 0 {
					m.logger.Info("AegisHub health recovered")
				}
				m.consecutiveFails = 0
			}
		}
	}
}

func (m *AegisHubMonitor) restart() {
	m.logger.Info("Restarting AegisHub VM...")

	if m.vm != nil {
		_ = m.vm.Stop()
	}

	// TODO: Re-create and start new VM using sandbox
	// For now we just log the intent
	m.logger.Warn("AegisHub restart requested (full re-launch not yet wired)")
}
