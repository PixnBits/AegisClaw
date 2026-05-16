package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
)

func TestCLIEvalProbeAuditContainsSignedActionPayload(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	probe := &cliEvalProbe{env: env}
	start := time.Now().Add(-time.Second)

	action := kernel.NewAction(kernel.ActionMemoryStore, "test", []byte(`{"key":"eval"}`))
	if _, err := env.Kernel.SignAndLog(action); err != nil {
		t.Fatalf("SignAndLog: %v", err)
	}

	found, err := probe.AuditContains(context.Background(), string(kernel.ActionMemoryStore), start)
	if err != nil {
		t.Fatalf("AuditContains: %v", err)
	}
	if !found {
		t.Fatal("expected AuditContains to find signed action payload")
	}
}

func TestSetTimerAcceptsScheduleAliasForCron(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	bus, err := eventbus.New(eventbus.Config{
		Dir:              t.TempDir(),
		MaxPendingTimers: 10,
		MaxSubscriptions: 10,
	})
	if err != nil {
		t.Fatalf("eventbus.New: %v", err)
	}
	env.EventBus = bus

	reg := &ToolRegistry{env: env}
	registerEventBusTools(reg, env)

	out, err := reg.Execute(context.Background(), "set_timer", `{
		"name":"daily-summary",
		"schedule":"@daily",
		"payload":{"type":"summary"},
		"task_id":"eval-summary"
	}`)
	if err != nil {
		t.Fatalf("set_timer: %v", err)
	}
	if !strings.Contains(out, "Timer set") {
		t.Fatalf("unexpected output: %s", out)
	}
	timers := bus.ListTimers(eventbus.TimerActive)
	if len(timers) != 1 {
		t.Fatalf("timers len = %d, want 1", len(timers))
	}
	if timers[0].Cron != "@daily" {
		t.Fatalf("cron = %q, want @daily", timers[0].Cron)
	}
}

func TestEvalReportMarshalStillJSON(t *testing.T) {
	data, err := marshalEvalReport(&evalReportJSON{RunID: "test"})
	if err != nil {
		t.Fatalf("marshalEvalReport: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}
