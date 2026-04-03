package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"go.uber.org/zap"
)

// startEventBusDaemon launches the background event bus timer daemon.
// It polls for due timers every minute, fires them, audits the events, and
// stores a memory entry for each wakeup so the agent has context on restart.
//
// The goroutine exits when ctx is cancelled (daemon shutdown).
func startEventBusDaemon(ctx context.Context, env *runtimeEnv) {
	if env.EventBus == nil {
		return
	}

	// Register the wakeup dispatcher: called synchronously by CheckAndFire
	// for each fired timer.
	env.EventBus.SetWakeupFunc(func(e eventbus.FiredEvent) {
		dispatchTimerWakeup(env, e)
	})

	go func() {
		// Run once immediately to catch any timers that fired while the daemon
		// was offline (at-least-once semantics).
		env.EventBus.CheckAndFire()

		ticker := time.NewTicker(eventbus.TimerCheckInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				env.EventBus.CheckAndFire()
			}
		}
	}()
}

// dispatchTimerWakeup handles a single fired timer event: audits it, writes a
// memory entry so the agent remembers the wakeup on its next turn, and logs
// a wakeup notice.
//
// Full snapshot restore is stubbed here pending Phase 3 Worker spawning; the
// agent will pick up the memory entry on its next chat turn.
func dispatchTimerWakeup(env *runtimeEnv, e eventbus.FiredEvent) {
	t := e.Timer
	sig := e.Signal

	env.Logger.Info("timer fired",
		zap.String("timer_id", t.TimerID),
		zap.String("name", t.Name),
		zap.String("task_id", t.TaskID),
		zap.String("signal_id", sig.SignalID),
	)

	// Merkle-audit the fired event.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"timer_id":  t.TimerID,
		"name":      t.Name,
		"task_id":   t.TaskID,
		"signal_id": sig.SignalID,
		"fired_at":  sig.ReceivedAt.Format(time.RFC3339),
	})
	act := kernel.NewAction(kernel.ActionEventTimerFired, "daemon", auditPayload)
	env.Kernel.SignAndLog(act) //nolint:errcheck

	// Write a memory entry so the agent picks up this wakeup on the next turn.
	if env.MemoryStore != nil {
		payloadDesc := ""
		if len(t.Payload) > 0 {
			payloadDesc = fmt.Sprintf(" payload=%s", truncate(string(t.Payload), 100))
		}
		memValue := fmt.Sprintf("Timer '%s' (id=%s) fired at %s.%s",
			t.Name, t.TimerID, sig.ReceivedAt.Format(time.RFC3339), payloadDesc)
		env.MemoryStore.Store(&memory.MemoryEntry{ //nolint:errcheck
			Key:    "timer.fired:" + t.TimerID,
			Value:  memValue,
			Tags:   []string{"timer", "wakeup", t.TaskID},
			TaskID: t.TaskID,
		})
	}
}
