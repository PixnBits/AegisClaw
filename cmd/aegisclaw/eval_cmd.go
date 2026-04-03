package main

import (
"context"
"encoding/json"
"fmt"
"time"

"github.com/PixnBits/AegisClaw/internal/audit"
"github.com/PixnBits/AegisClaw/internal/eval"
"github.com/spf13/cobra"
)

var evalCmd = &cobra.Command{
Use:   "eval",
Short: "Run the agentic evaluation harness",
}

var evalRunScenarioFlag string

var evalRunCmd = &cobra.Command{
Use:   "run",
Short: "Run evaluation scenarios",
RunE:  runEvalRun,
}

var evalReportCmd = &cobra.Command{
Use:   "report",
Short: "Print a summary of the last evaluation run",
RunE: func(_ *cobra.Command, _ []string) error {
fmt.Println("No saved eval report. Run `aegisclaw eval run` to generate one.")
return nil
},
}

func init() {
evalRunCmd.Flags().StringVar(&evalRunScenarioFlag, "scenario", "",
"Run only this scenario (background_research | oss_issue_to_pr | recurring_summary)")
}

func runEvalRun(cmd *cobra.Command, _ []string) error {
env, err := initRuntime()
if err != nil {
return err
}
defer env.Logger.Sync()

probe := &cliEvalProbe{env: env}
runner := eval.NewRunner()

var report *eval.EvalReport
if evalRunScenarioFlag != "" {
result, err := runner.RunScenario(cmd.Context(), eval.ScenarioID(evalRunScenarioFlag), probe)
if err != nil {
return fmt.Errorf("run scenario: %w", err)
}
report = &eval.EvalReport{
RunID:     fmt.Sprintf("cli-%d", time.Now().Unix()),
StartedAt: time.Now().UTC(),
Results:   []eval.ScenarioResult{result},
Total:     1,
}
report.FinishedAt = time.Now().UTC()
if result.Passed {
report.Passed = 1
} else {
report.Failed = 1
}
} else {
report = runner.RunAll(cmd.Context(), probe)
}

printEvalReport(report)
return nil
}

func printEvalReport(report *eval.EvalReport) {
if report.RunID != "" {
fmt.Printf("Run: %s\n", report.RunID)
}
fmt.Printf("Results: %d/%d passed\n\n", report.Passed, report.Total)
for _, r := range report.Results {
status := "PASS"
if !r.Passed {
status = "FAIL"
}
fmt.Printf("  [%s] %s — %s (%dms)\n", status, r.ScenarioID, r.Name, r.DurationMS)
for _, c := range r.Criteria {
cs := "  PASS"
if !c.Passed {
cs = "  FAIL"
}
msg := ""
if c.Message != "" {
msg = ": " + c.Message
}
fmt.Printf("    %s  %s%s\n", cs, c.Name, msg)
}
if r.Error != "" {
fmt.Printf("      error: %s\n", r.Error)
}
}
}

// cliEvalProbe implements eval.DaemonProbe using the in-process runtimeEnv.
type cliEvalProbe struct {
env *runtimeEnv
}

func (p *cliEvalProbe) Chat(_ context.Context, _ string) (string, error) {
return "", fmt.Errorf("chat not supported in CLI eval mode")
}

func (p *cliEvalProbe) CallTool(ctx context.Context, tool, args string) (string, error) {
if p.env.Kernel == nil {
return "", fmt.Errorf("runtime not fully initialized")
}
tr := buildToolRegistry(p.env)
return tr.Execute(ctx, tool, args)
}

func (p *cliEvalProbe) GetMemory(_ context.Context, key string) (string, error) {
if p.env.MemoryStore == nil {
return "", fmt.Errorf("memory store not available")
}
entries, err := p.env.MemoryStore.Retrieve(key, 1, "")
if err != nil {
return "", err
}
if len(entries) == 0 {
return "", fmt.Errorf("memory key not found: %s", key)
}
return entries[0].Value, nil
}

func (p *cliEvalProbe) AuditContains(_ context.Context, actionType string, since time.Time) (bool, error) {
if p.env.Kernel == nil {
return false, nil
}
logPath := p.env.Kernel.AuditLog().Path()
entries, err := audit.ReadEntries(logPath)
if err != nil {
return false, err
}
for _, e := range entries {
// Parse the action type from the payload JSON.
var payload struct {
ActionType string `json:"action_type"`
}
if jsonErr := json.Unmarshal(e.Payload, &payload); jsonErr == nil {
if payload.ActionType == actionType && e.Timestamp.After(since) {
return true, nil
}
}
}
return false, nil
}

// Ensure the JSON report type is accessible for future "eval export" use.
type evalReportJSON = eval.EvalReport

func marshalEvalReport(r *eval.EvalReport) ([]byte, error) {
return json.MarshalIndent(r, "", "  ")
}
