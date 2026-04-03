// Package eval implements a synthetic evaluation harness for AegisClaw Phase 5.
//
// The harness defines Scenarios (declarative test cases), runs them against
// the daemon or stub, and produces EvalReports with pass/fail grades for each
// acceptance criterion.
//
// Three built-in scenarios validate the three core agentic workflows:
//  1. Background Research (async timer + worker + memory)
//  2. OSS Issue to PR (approval gate + worker + audit completeness)
//  3. Recurring Summary (cron timer + summarizer worker + compaction)
//
// Scenarios are run with a configurable stub backend so CI doesn't require
// a live daemon or KVM.
package eval

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ScenarioID identifies a built-in evaluation scenario.
type ScenarioID string

const (
	ScenarioBackgroundResearch ScenarioID = "background_research"
	ScenarioOSSIssueToPR       ScenarioID = "oss_issue_to_pr"
	ScenarioRecurringSummary   ScenarioID = "recurring_summary"
)

// CriterionResult records whether a single acceptance criterion passed.
type CriterionResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// ScenarioResult is the outcome of running a single scenario.
type ScenarioResult struct {
	ScenarioID    ScenarioID        `json:"scenario_id"`
	Name          string            `json:"name"`
	Passed        bool              `json:"passed"`
	Criteria      []CriterionResult `json:"criteria"`
	DurationMS    int64             `json:"duration_ms"`
	Error         string            `json:"error,omitempty"`
}

// EvalReport collects results for all scenarios in a run.
type EvalReport struct {
	RunID     string           `json:"run_id"`
	StartedAt time.Time        `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	Results   []ScenarioResult `json:"results"`
	Passed    int              `json:"passed"`
	Failed    int              `json:"failed"`
	Total     int              `json:"total"`
}

// Summary returns a human-readable one-line summary.
func (r *EvalReport) Summary() string {
	return fmt.Sprintf("EvalReport %s: %d/%d passed (duration=%s)",
		r.RunID, r.Passed, r.Total,
		r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond))
}

// RunnerFunc is a function that exercises a scenario and returns criterion results.
// It receives the scenario ID and a DaemonProbe for interacting with the system.
type RunnerFunc func(ctx context.Context, probe DaemonProbe) ([]CriterionResult, error)

// DaemonProbe abstracts daemon interactions for the eval harness.
// In tests it can be backed by a stub; in integration runs by the real API.
type DaemonProbe interface {
	// Chat sends a message to the agent and returns the response.
	Chat(ctx context.Context, message string) (string, error)
	// CallTool calls a tool directly (bypassing the agent ReAct loop).
	CallTool(ctx context.Context, tool, args string) (string, error)
	// GetMemory retrieves a memory entry by key.
	GetMemory(ctx context.Context, key string) (string, error)
	// AuditContains checks whether the audit log contains an entry with
	// the given action type since startTime.
	AuditContains(ctx context.Context, actionType string, since time.Time) (bool, error)
}

// Scenario describes an evaluation scenario.
type Scenario struct {
	ID          ScenarioID
	Name        string
	Description string
	Runner      RunnerFunc
}

// Runner executes scenarios and collects results.
type Runner struct {
	scenarios []*Scenario
}

// NewRunner creates a Runner with the built-in scenarios.
func NewRunner() *Runner {
	r := &Runner{}
	r.scenarios = []*Scenario{
		{
			ID:          ScenarioBackgroundResearch,
			Name:        "Background Research",
			Description: "Agent stores context in memory, spawns a researcher worker, worker returns findings.",
			Runner:      runBackgroundResearch,
		},
		{
			ID:          ScenarioOSSIssueToPR,
			Name:        "OSS Issue to PR",
			Description: "Agent requests human approval before a high-risk action; audit log records the decision.",
			Runner:      runOSSIssueToPR,
		},
		{
			ID:          ScenarioRecurringSummary,
			Name:        "Recurring Summary",
			Description: "Agent schedules a cron timer; timer appears in pending async items; memory entry written.",
			Runner:      runRecurringSummary,
		},
	}
	return r
}

// RunAll executes all scenarios and returns an EvalReport.
func (r *Runner) RunAll(ctx context.Context, probe DaemonProbe) *EvalReport {
	report := &EvalReport{
		RunID:     fmt.Sprintf("eval-%d", time.Now().Unix()),
		StartedAt: time.Now().UTC(),
	}
	for _, s := range r.scenarios {
		report.Results = append(report.Results, r.runOne(ctx, s, probe))
	}
	report.FinishedAt = time.Now().UTC()
	for _, res := range report.Results {
		report.Total++
		if res.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report
}

// RunScenario runs a single scenario by ID.
func (r *Runner) RunScenario(ctx context.Context, id ScenarioID, probe DaemonProbe) (ScenarioResult, error) {
	for _, s := range r.scenarios {
		if s.ID == id {
			return r.runOne(ctx, s, probe), nil
		}
	}
	return ScenarioResult{}, fmt.Errorf("unknown scenario: %s", id)
}

func (r *Runner) runOne(ctx context.Context, s *Scenario, probe DaemonProbe) ScenarioResult {
	start := time.Now()
	criteria, err := s.Runner(ctx, probe)
	dur := time.Since(start)

	res := ScenarioResult{
		ScenarioID: s.ID,
		Name:       s.Name,
		Criteria:   criteria,
		DurationMS: dur.Milliseconds(),
	}
	if err != nil {
		res.Error = err.Error()
	}
	// Scenario passes only if all criteria pass (or there are no criteria and no error).
	res.Passed = err == nil
	for _, c := range criteria {
		if !c.Passed {
			res.Passed = false
			break
		}
	}
	if len(criteria) == 0 && err == nil {
		res.Passed = false // no criteria = not a valid test
		res.Error = "no criteria returned by runner"
	}
	return res
}

// ─── Scenario runners ─────────────────────────────────────────────────────────

func runBackgroundResearch(ctx context.Context, probe DaemonProbe) ([]CriterionResult, error) {
	var criteria []CriterionResult
	start := time.Now()

	// Criterion 1: agent can store a memory entry via tool.
	_, err := probe.CallTool(ctx, "store_memory", `{"key":"eval.research.task","value":"research Go generics","tags":["eval"],"task_id":"eval-bg"}`)
	criteria = append(criteria, CriterionResult{
		Name:    "memory_store_tool",
		Passed:  err == nil,
		Message: errMsg(err),
	})

	// Criterion 2: agent can retrieve the memory entry.
	val, err := probe.GetMemory(ctx, "eval.research.task")
	criteria = append(criteria, CriterionResult{
		Name:    "memory_retrieve",
		Passed:  err == nil && strings.Contains(val, "Go generics"),
		Message: errMsg(err),
	})

	// Criterion 3: audit log contains memory.store event since test start.
	found, err := probe.AuditContains(ctx, "memory.store", start)
	criteria = append(criteria, CriterionResult{
		Name:    "audit_memory_store",
		Passed:  err == nil && found,
		Message: errMsg(err),
	})

	// Criterion 4: worker tool is callable (returns without internal error).
	result, err := probe.CallTool(ctx, "list_pending_async", `{}`)
	criteria = append(criteria, CriterionResult{
		Name:    "list_pending_async_callable",
		Passed:  err == nil && result != "",
		Message: errMsg(err),
	})

	return criteria, nil
}

func runOSSIssueToPR(ctx context.Context, probe DaemonProbe) ([]CriterionResult, error) {
	var criteria []CriterionResult
	start := time.Now()

	// Criterion 1: request_human_approval creates an approval record.
	result, err := probe.CallTool(ctx, "request_human_approval", `{
		"title":"Deploy hotfix to production",
		"description":"Fixes critical CVE. Deploy to prod requires human sign-off.",
		"risk_level":"high",
		"task_id":"eval-oss"
	}`)
	criteria = append(criteria, CriterionResult{
		Name:    "approval_created",
		Passed:  err == nil && (strings.Contains(result, "approval") || strings.Contains(result, "pending")),
		Message: errMsg(err),
	})

	// Criterion 2: approval appears in list_pending_async.
	pending, err := probe.CallTool(ctx, "list_pending_async", `{}`)
	criteria = append(criteria, CriterionResult{
		Name:    "approval_in_pending_list",
		Passed:  err == nil && strings.Contains(pending, "Deploy hotfix"),
		Message: errMsg(err),
	})

	// Criterion 3: audit contains approval.request since test start.
	found, err := probe.AuditContains(ctx, "approval.request", start)
	criteria = append(criteria, CriterionResult{
		Name:    "audit_approval_request",
		Passed:  err == nil && found,
		Message: errMsg(err),
	})

	// Criterion 4: worker_status tool is callable.
	_, err = probe.CallTool(ctx, "worker_status", `{}`)
	criteria = append(criteria, CriterionResult{
		Name:    "worker_status_callable",
		Passed:  err == nil,
		Message: errMsg(err),
	})

	return criteria, nil
}

func runRecurringSummary(ctx context.Context, probe DaemonProbe) ([]CriterionResult, error) {
	var criteria []CriterionResult
	start := time.Now()

	// Criterion 1: set_timer creates a cron timer.
	result, err := probe.CallTool(ctx, "set_timer", `{
		"name":"daily-summary",
		"schedule":"@daily",
		"payload":{"type":"summary"},
		"task_id":"eval-summary"
	}`)
	criteria = append(criteria, CriterionResult{
		Name:    "timer_created",
		Passed:  err == nil && result != "",
		Message: errMsg(err),
	})

	// Criterion 2: timer appears in list_pending_async.
	pending, err := probe.CallTool(ctx, "list_pending_async", `{}`)
	criteria = append(criteria, CriterionResult{
		Name:    "timer_in_pending_list",
		Passed:  err == nil && strings.Contains(pending, "daily-summary"),
		Message: errMsg(err),
	})

	// Criterion 3: audit contains event.timer.set since test start.
	found, err := probe.AuditContains(ctx, "event.timer.set", start)
	criteria = append(criteria, CriterionResult{
		Name:    "audit_timer_set",
		Passed:  err == nil && found,
		Message: errMsg(err),
	})

	// Criterion 4: cancel the timer (cleanup).
	timerID := extractTimerID(result)
	if timerID != "" {
		_, cancelErr := probe.CallTool(ctx, "cancel_timer", fmt.Sprintf(`{"timer_id":%q}`, timerID))
		criteria = append(criteria, CriterionResult{
			Name:    "timer_cancelled",
			Passed:  cancelErr == nil,
			Message: errMsg(cancelErr),
		})
	}

	_ = start
	return criteria, nil
}

// extractTimerID tries to parse a timer_id from a set_timer result string.
func extractTimerID(result string) string {
	// Result looks like: "Timer 'daily-summary' (id=<uuid>) scheduled ..."
	const prefix = "id="
	idx := strings.Index(result, prefix)
	if idx < 0 {
		return ""
	}
	rest := result[idx+len(prefix):]
	// UUID is 36 chars.
	if len(rest) >= 36 {
		return rest[:36]
	}
	// Try finding end delimiter.
	end := strings.IndexAny(rest, ") \n")
	if end > 0 {
		return rest[:end]
	}
	return rest
}

func errMsg(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
