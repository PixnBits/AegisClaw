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

// timerSpawnPayload is the optional worker-spawn directive that the agent can
// embed in a timer's Payload field when calling set_timer.  When a timer fires
// and its payload contains a non-empty task_description, the daemon spawns an
// ephemeral worker VM to continue the task autonomously rather than merely
// writing a passive memory entry and waiting for the human's next chat turn.
//
// Example set_timer invocation that uses autonomous wakeup:
//
//	{
//	  "name": "summarise_issues",
//	  "trigger_at": "2024-06-01T09:00:00Z",
//	  "task_id": "T-123",
//	  "payload": {
//	    "task_description": "Summarise all open GitHub issues and store findings",
//	    "role": "summarizer",
//	    "timeout_mins": 10
//	  }
//	}
type timerSpawnPayload struct {
	TaskDescription string   `json:"task_description"`
	Role            string   `json:"role,omitempty"`
	TimeoutMins     int      `json:"timeout_mins,omitempty"`
	ToolsGranted    []string `json:"tools_granted,omitempty"`
}

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
// memory entry so the agent has context, and — when the timer payload contains
// a task_description — spawns an ephemeral worker VM to continue the task
// autonomously without waiting for the human's next chat turn.
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

	// Write a memory entry so the agent always has context about this wakeup,
	// regardless of whether a worker is spawned.
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

	// If the timer payload carries a task_description, spawn an ephemeral
	// worker VM to execute the task autonomously.  The worker runs in a
	// separate goroutine so the timer-check loop is never blocked.
	if len(t.Payload) > 0 {
		var sp timerSpawnPayload
		if err := json.Unmarshal(t.Payload, &sp); err != nil {
			env.Logger.Warn("timer wakeup: payload is not valid JSON, skipping worker spawn",
				zap.String("timer_id", t.TimerID),
				zap.Error(err),
			)
		} else if sp.TaskDescription != "" {
			go dispatchTimerWorker(env, t.TimerID, t.TaskID, sp)
		}
	}
}

// dispatchTimerWorker spawns an ephemeral worker VM for a timer-driven task
// and stores the result in memory when the worker finishes.  It is always
// called in a dedicated goroutine so the timer-check loop remains responsive.
func dispatchTimerWorker(env *runtimeEnv, timerID, taskID string, sp timerSpawnPayload) {
	// Guard: a Runtime and Config are required to launch a worker VM.  In
	// environments where Firecracker is not configured (e.g. tests without KVM),
	// we log a warning and skip the spawn rather than panicking.
	if env.Runtime == nil || env.Config == nil {
		env.Logger.Warn("timer wakeup: Runtime or Config unavailable, skipping worker spawn",
			zap.String("timer_id", timerID),
			zap.String("task_id", taskID),
		)
		return
	}
	env.Logger.Info("timer wakeup: spawning worker",
		zap.String("timer_id", timerID),
		zap.String("task_id", taskID),
		zap.String("role", sp.Role),
		zap.String("task_preview", truncate(sp.TaskDescription, 80)),
	)

	params := spawnWorkerParams{
		TaskDescription: sp.TaskDescription,
		Role:            sp.Role,
		TimeoutMins:     sp.TimeoutMins,
		ToolsGranted:    sp.ToolsGranted,
		TaskID:          taskID,
	}

	// Use a background context: spawnWorker enforces its own deadline derived
	// from params.TimeoutMins, so the outer context only needs to outlive it.
	result, err := spawnWorker(context.Background(), env, params)
	if err != nil {
		env.Logger.Error("timer-spawned worker failed",
			zap.String("timer_id", timerID),
			zap.String("task_id", taskID),
			zap.Error(err),
		)
		if env.MemoryStore != nil {
			env.MemoryStore.Store(&memory.MemoryEntry{ //nolint:errcheck
				Key:    "timer.worker.error:" + timerID,
				Value:  fmt.Sprintf("Timer '%s' worker failed: %v", timerID, err),
				Tags:   []string{"timer", "worker_error", taskID},
				TaskID: taskID,
			})
		}
		return
	}

	env.Logger.Info("timer-spawned worker completed",
		zap.String("timer_id", timerID),
		zap.String("task_id", taskID),
		zap.String("result_preview", truncate(result, 200)),
	)
	if env.MemoryStore != nil {
		env.MemoryStore.Store(&memory.MemoryEntry{ //nolint:errcheck
			Key:    "timer.worker.result:" + timerID,
			Value:  result,
			Tags:   []string{"timer", "worker_result", taskID},
			TaskID: taskID,
		})
	}
}
