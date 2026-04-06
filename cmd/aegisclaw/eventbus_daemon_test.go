package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"go.uber.org/zap/zaptest"
)

// newTestMemoryStore creates a real in-process memory store backed by a temp
// directory and an ephemeral age identity.
func newTestMemoryStore(t *testing.T) *memory.Store {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	store, err := memory.NewStore(memory.StoreConfig{Dir: t.TempDir()}, identity)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	return store
}

// makeTimerEvent builds a minimal FiredEvent for test purposes.
func makeTimerEvent(timerID, name, taskID string, payload json.RawMessage) eventbus.FiredEvent {
	return eventbus.FiredEvent{
		Timer: &eventbus.Timer{
			TimerID: timerID,
			Name:    name,
			TaskID:  taskID,
			Payload: payload,
		},
		Signal: &eventbus.Signal{
			SignalID:    "sig-" + timerID,
			TimerID:     timerID,
			TaskID:      taskID,
			ReceivedAt:  time.Now().UTC(),
		},
	}
}

// makeTestRuntimeForTimer returns a minimal runtimeEnv that is sufficient for
// dispatchTimerWakeup (kernel + memory store).  Runtime is intentionally nil
// so that any code path reaching spawnWorker short-circuits via the guard.
func makeTestRuntimeForTimer(t *testing.T) *runtimeEnv {
	t.Helper()
	kernel.ResetInstance()
	t.Cleanup(func() { kernel.ResetInstance() })

	logger := zaptest.NewLogger(t)
	kern, err := kernel.GetInstance(logger, t.TempDir())
	if err != nil {
		t.Fatalf("kernel.GetInstance: %v", err)
	}
	t.Cleanup(func() { kern.Shutdown() })

	return &runtimeEnv{
		Logger:      logger,
		Kernel:      kern,
		MemoryStore: newTestMemoryStore(t),
		// Runtime intentionally nil — triggers the guard in dispatchTimerWorker.
	}
}

// TestDispatchTimerWakeup_NoPayload verifies that a timer fired with no payload
// writes exactly the "timer.fired:" memory entry and does not spawn a worker.
func TestDispatchTimerWakeup_NoPayload(t *testing.T) {
	env := makeTestRuntimeForTimer(t)
	evt := makeTimerEvent("tid-1", "my-timer", "task-99", nil)

	dispatchTimerWakeup(env, evt)

	entries, err := env.MemoryStore.List("")
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	// Expect exactly one memory entry: "timer.fired:tid-1"
	if len(entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d: %v", len(entries), entries)
	}
	if entries[0].Key != "timer.fired:tid-1" {
		t.Errorf("expected key 'timer.fired:tid-1', got %q", entries[0].Key)
	}
	// Retrieve the full entry to check the value contains the timer name.
	full, err := env.MemoryStore.Retrieve("my-timer", 5, "")
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(full) == 0 || !strings.Contains(full[0].Value, "my-timer") {
		t.Errorf("expected value to mention timer name 'my-timer', got %v", full)
	}
}

// TestDispatchTimerWakeup_PayloadNoTaskDescription verifies that a timer payload
// without a task_description field does not trigger a worker spawn.
func TestDispatchTimerWakeup_PayloadNoTaskDescription(t *testing.T) {
	env := makeTestRuntimeForTimer(t)
	raw, _ := json.Marshal(map[string]string{"other_field": "some value"})
	evt := makeTimerEvent("tid-2", "cron-timer", "task-42", raw)

	dispatchTimerWakeup(env, evt)

	entries, err := env.MemoryStore.List("")
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	// Only the passive wakeup entry; no worker.* keys.
	for _, e := range entries {
		if strings.HasPrefix(e.Key, "timer.worker.") {
			t.Errorf("unexpected worker memory entry: %q", e.Key)
		}
	}
	if len(entries) != 1 || entries[0].Key != "timer.fired:tid-2" {
		t.Errorf("unexpected memory entries: %v", entries)
	}
}

// TestDispatchTimerWakeup_WithTaskDescription verifies that a timer payload
// carrying a task_description triggers dispatchTimerWorker.  Because Runtime
// is nil in this test environment the worker guard fires immediately, so we
// verify that:
//  1. The wakeup memory entry is still written.
//  2. No panic occurs from reaching sandbox code.
//  3. No stray worker-result entry is written (spawn was guarded, not run).
func TestDispatchTimerWakeup_WithTaskDescription(t *testing.T) {
	env := makeTestRuntimeForTimer(t)

	sp := timerSpawnPayload{
		TaskDescription: "Summarise open issues and store findings",
		Role:            "summarizer",
		TimeoutMins:     5,
	}
	raw, _ := json.Marshal(sp)
	evt := makeTimerEvent("tid-3", "scheduled-summary", "task-77", raw)

	// Must not panic.
	dispatchTimerWakeup(env, evt)

	// Allow a brief moment for the goroutine launched by dispatchTimerWakeup to
	// run and reach the nil-Runtime guard.
	time.Sleep(50 * time.Millisecond)

	entries, err := env.MemoryStore.List("")
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}

	// Wakeup entry must always be present.
	found := false
	for _, e := range entries {
		if e.Key == "timer.fired:tid-3" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'timer.fired:tid-3' memory entry not found")
	}

	// No worker result entry because the nil-Runtime guard fired.
	for _, e := range entries {
		if strings.HasPrefix(e.Key, "timer.worker.result:") {
			t.Errorf("unexpected worker result entry (Runtime is nil): key=%q", e.Key)
		}
	}
}

// TestTimerSpawnPayload_Parsing verifies that timerSpawnPayload round-trips
// through JSON correctly, including zero-value / partial payloads.
func TestTimerSpawnPayload_Parsing(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantTask    string
		wantRole    string
		wantTimeout int
	}{
		{
			name:        "full payload",
			raw:         `{"task_description":"do stuff","role":"researcher","timeout_mins":15}`,
			wantTask:    "do stuff",
			wantRole:    "researcher",
			wantTimeout: 15,
		},
		{
			name:     "task only",
			raw:      `{"task_description":"minimal task"}`,
			wantTask: "minimal task",
		},
		{
			name: "empty object",
			raw:  `{}`,
		},
		{
			name: "unrelated fields ignored",
			raw:  `{"foo":"bar","baz":42}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sp timerSpawnPayload
			if err := json.Unmarshal([]byte(tc.raw), &sp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if sp.TaskDescription != tc.wantTask {
				t.Errorf("TaskDescription: got %q, want %q", sp.TaskDescription, tc.wantTask)
			}
			if sp.Role != tc.wantRole {
				t.Errorf("Role: got %q, want %q", sp.Role, tc.wantRole)
			}
			if sp.TimeoutMins != tc.wantTimeout {
				t.Errorf("TimeoutMins: got %d, want %d", sp.TimeoutMins, tc.wantTimeout)
			}
		})
	}
}
