package main

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
				m.logger.Warn("AegisHub health check failed", zap.Error(err))
				// TODO: Trigger restart logic here in future
			} else {
				m.logger.Debug("AegisHub health OK")
			}
		}
	}
}
