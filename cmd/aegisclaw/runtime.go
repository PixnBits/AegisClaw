package main

// AegisHubMonitor now holds VM for full lifecycle control.
type AegisHubMonitor struct {
	client aegishub.Client
	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
	vm     interface{ Stop() error } // sandbox VM
}

func (m *AegisHubMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	if m.vm != nil {
		m.logger.Info("Stopping AegisHub Firecracker VM")
		_ = m.vm.Stop()
	}

	if m.client != nil {
		m.client.Close()
	}
	m.logger.Info("AegisHub fully stopped")
}
