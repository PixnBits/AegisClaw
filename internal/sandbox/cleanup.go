package sandbox

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// Cleanup stops all running sandboxes and tears down their networks.
// This is called on kernel shutdown to ensure no orphaned VMs or network
// resources remain. Errors are logged but do not prevent cleanup of
// remaining sandboxes.
func (r *FirecrackerRuntime) Cleanup(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var stopped int
	for id, ms := range r.sandboxes {
		if ms.info.State != StateRunning {
			continue
		}

		r.logger.Info("cleaning up running sandbox", zap.String("id", id))

		if ms.machine != nil {
			shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := ms.machine.Shutdown(shutCtx); err != nil {
				r.logger.Warn("graceful shutdown failed during cleanup, forcing",
					zap.String("id", id),
					zap.Error(err),
				)
				if err := ms.machine.StopVMM(); err != nil {
					r.logger.Error("forced stop failed during cleanup",
						zap.String("id", id),
						zap.Error(err),
					)
				}
			}
			cancel()
		}

		if ms.cancel != nil {
			ms.cancel()
		}

		if ms.info.TapDevice != "" {
			r.teardownNetwork(ms.info.TapDevice, id)
		}

		now := time.Now().UTC()
		ms.info.State = StateStopped
		ms.info.StoppedAt = &now
		ms.info.PID = 0
		ms.machine = nil
		ms.cancel = nil
		stopped++
	}

	if stopped > 0 {
		if err := r.saveState(); err != nil {
			r.logger.Error("failed to persist state after cleanup", zap.Error(err))
		}
		r.logger.Info("cleanup complete", zap.Int("sandboxes_stopped", stopped))
	}
}
