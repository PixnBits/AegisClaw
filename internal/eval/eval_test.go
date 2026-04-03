package eval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/eval"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
)

// memStub is a simple in-memory key-value store for test purposes.
type memStub struct {
	mu   sync.RWMutex
	data map[string]string
}

func (m *memStub) store(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
}

func (m *memStub) get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

// testProbe is an in-process DaemonProbe backed by in-memory stubs.
type testProbe struct {
	mem *memStub
	bus *eventbus.Bus
}

func (p *testProbe) Chat(_ context.Context, _ string) (string, error) { return "ok", nil }

func (p *testProbe) CallTool(_ context.Context, tool, args string) (string, error) {
	switch tool {
	case "store_memory":
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(args), &req); err != nil {
			return "", err
		}
		p.mem.store(req.Key, req.Value)
		return "stored key=" + req.Key, nil

	case "retrieve_memory":
		var req struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(args), &req); err != nil {
			return "", err
		}
		v, ok := p.mem.get(req.Key)
		if !ok {
			return "", fmt.Errorf("key not found: %s", req.Key)
		}
		return v, nil

	case "list_pending_async":
		timers := p.bus.ListTimers("")
		approvals := p.bus.ListApprovals()
		var parts []string
		for _, t := range timers {
			parts = append(parts, fmt.Sprintf("timer:%s(%s)", t.Name, t.TimerID))
		}
		for _, a := range approvals {
			parts = append(parts, fmt.Sprintf("approval:%s(%s)", a.Title, a.ApprovalID))
		}
		if len(parts) == 0 {
			return "No pending async items.", nil
		}
		return strings.Join(parts, "\n"), nil

	case "request_human_approval":
		var req struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			RiskLevel   string `json:"risk_level"`
			TaskID      string `json:"task_id"`
		}
		if err := json.Unmarshal([]byte(args), &req); err != nil {
			return "", err
		}
		a, err := p.bus.RequestApproval(req.Title, req.Description, req.RiskLevel, "eval", req.TaskID, nil, 0)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("approval pending id=%s", a.ApprovalID), nil

	case "set_timer":
		var req struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			TaskID   string `json:"task_id"`
		}
		if err := json.Unmarshal([]byte(args), &req); err != nil {
			return "", err
		}
		t, err := p.bus.SetTimer(eventbus.SetTimerParams{Name: req.Name, Cron: req.Schedule, TaskID: req.TaskID})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("timer created id=%s", t.TimerID), nil

	case "cancel_timer":
		var req struct {
			TimerID string `json:"timer_id"`
		}
		if err := json.Unmarshal([]byte(args), &req); err != nil {
			return "", err
		}
		_, err := p.bus.CancelTimer(req.TimerID)
		return "cancelled", err

	case "worker_status":
		return "No workers found.", nil

	default:
		return "", fmt.Errorf("unknown tool: %s", tool)
	}
}

func (p *testProbe) GetMemory(_ context.Context, key string) (string, error) {
	v, ok := p.mem.get(key)
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return v, nil
}

func (p *testProbe) AuditContains(_ context.Context, _ string, _ time.Time) (bool, error) {
	return true, nil // unit tests: no real audit log, assume present
}

func newTestProbe(t *testing.T) *testProbe {
	t.Helper()
	bus, err := eventbus.New(eventbus.Config{Dir: t.TempDir(), MaxPendingTimers: 10, MaxSubscriptions: 10})
	if err != nil {
		t.Fatalf("eventbus.New: %v", err)
	}
	return &testProbe{
		mem: &memStub{data: make(map[string]string)},
		bus: bus,
	}
}

func TestEval_BackgroundResearch(t *testing.T) {
	runner := eval.NewRunner()
	result, err := runner.RunScenario(context.Background(), eval.ScenarioBackgroundResearch, newTestProbe(t))
	if err != nil {
		t.Fatalf("RunScenario: %v", err)
	}
	for _, c := range result.Criteria {
		if !c.Passed {
			t.Errorf("criterion %q failed: %s", c.Name, c.Message)
		}
	}
	if !result.Passed {
		t.Errorf("scenario did not pass: %s", result.Error)
	}
}

func TestEval_OSSIssueToPR(t *testing.T) {
	runner := eval.NewRunner()
	result, err := runner.RunScenario(context.Background(), eval.ScenarioOSSIssueToPR, newTestProbe(t))
	if err != nil {
		t.Fatalf("RunScenario: %v", err)
	}
	for _, c := range result.Criteria {
		if !c.Passed {
			t.Errorf("criterion %q failed: %s", c.Name, c.Message)
		}
	}
}

func TestEval_RecurringSummary(t *testing.T) {
	runner := eval.NewRunner()
	result, err := runner.RunScenario(context.Background(), eval.ScenarioRecurringSummary, newTestProbe(t))
	if err != nil {
		t.Fatalf("RunScenario: %v", err)
	}
	for _, c := range result.Criteria {
		if !c.Passed {
			t.Errorf("criterion %q failed: %s", c.Name, c.Message)
		}
	}
}

func TestEval_RunAll(t *testing.T) {
	runner := eval.NewRunner()
	report := runner.RunAll(context.Background(), newTestProbe(t))
	if report.Total != 3 {
		t.Errorf("expected 3 scenarios, got %d", report.Total)
	}
	if report.Summary() == "" {
		t.Error("empty summary")
	}
	t.Logf("Eval report: %s", report.Summary())
	for _, r := range report.Results {
		t.Logf("  [%s] %s: passed=%v error=%s", r.ScenarioID, r.Name, r.Passed, r.Error)
	}
}
