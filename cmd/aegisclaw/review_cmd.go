package main

// review_cmd.go provides the `aegisclaw review` command group with sub-commands
// for listing, running, and disabling built-in periodic security review skills.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// reviewCmd is the root command for the built-in security review framework.
var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Manage built-in periodic security review skills",
	Long: `Commands for the AegisClaw built-in periodic security review framework.

Four review skills run automatically on a configurable schedule:
  security-auditor            Daily   — audit log delta review (CISO lead)
  access-permission-reviewer  Monthly — deployed skills & permission scan
  secrets-rotation-verifier   Weekly  — secret rotation policy check
  policy-threat-model-refresher Quarterly — governance rule refresh

  review list     Show all review skills and their next scheduled run
  review run      Trigger an immediate, on-demand review
  review disable  Disable a review (logged to the audit trail)`,
}

// reviewListCmd lists all built-in review skills with status and next fire time.
var reviewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List built-in review skills and their schedule",
	Long:  `Displays all four built-in review skills, their enable/disable state, and the next scheduled run time.`,
	RunE:  runReviewList,
}

// reviewRunCmd triggers an on-demand, immediate run of a named review skill.
var reviewRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Trigger an immediate run of a review skill",
	Long: `Spawns an ephemeral worker that performs the named review immediately,
regardless of its scheduled cadence.  Results are written to the immutable
Merkle audit log with a signed, timestamped entry.

Available review names:
  security-auditor
  access-permission-reviewer
  secrets-rotation-verifier
  policy-threat-model-refresher`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewRun,
}

// reviewDisableCmd disables a named review skill and cancels its cron timer.
var reviewDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a built-in review skill (logged to audit trail)",
	Long: `Marks a review skill as disabled and cancels its cron timer.
This action is recorded in the immutable Merkle audit log.
Re-enable by removing the disabled flag (requires daemon restart).`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewDisable,
}

// reviewEnableCmd re-enables a previously disabled review skill.
var reviewEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Re-enable a disabled built-in review skill",
	Long: `Clears the disabled flag on a review skill. The cron timer will be
re-registered on the next daemon restart or when the skill is re-bootstrapped.`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewEnable,
}

func runReviewList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	type reviewStatus struct {
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Cadence     string     `json:"cadence"`
		Disabled    bool       `json:"disabled"`
		NextFireAt  *time.Time `json:"next_fire_at,omitempty"`
		TimerID     string     `json:"timer_id,omitempty"`
	}

	// Build map of active timer next-fire times.
	timerNext := make(map[string]*time.Time)
	timerID := make(map[string]string)
	if env.EventBus != nil {
		for _, t := range env.EventBus.ListTimers(eventbus.TimerActive) {
			if t.Owner == reviewTimerOwner {
				timerNext[t.Name] = t.NextFireAt
				timerID[t.Name] = t.TimerID
			}
		}
	}

	knownNames := []string{
		reviewSkillSecurityAuditor,
		reviewSkillAccessReviewer,
		reviewSkillSecretsVerifier,
		reviewSkillPolicyRefresher,
	}

	var results []reviewStatus
	for _, name := range knownNames {
		entry, ok := env.Registry.Get(name)
		rs := reviewStatus{Name: name}
		if ok {
			rs.Description = entry.Metadata[reviewMetadataKeyDescription]
			rs.Cadence = entry.Metadata[reviewMetadataKeyCadence]
			rs.Disabled = entry.Metadata[reviewMetadataKeyDisabled] == "true"
		}
		rs.NextFireAt = timerNext[name]
		rs.TimerID = timerID[name]
		results = append(results, rs)
	}

	if globalJSON {
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("%-38s %-12s %-10s %s\n", "SKILL", "CADENCE", "STATUS", "NEXT RUN")
	for _, rs := range results {
		status := "enabled"
		if rs.Disabled {
			status = "disabled"
		}
		next := "–"
		if rs.NextFireAt != nil {
			next = rs.NextFireAt.Local().Format("2006-01-02 15:04")
		}
		fmt.Printf("%-38s %-12s %-10s %s\n", rs.Name, rs.Cadence, status, next)
	}
	return nil
}

func runReviewRun(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Validate the name is a known review skill.
	known := map[string]bool{
		reviewSkillSecurityAuditor: true,
		reviewSkillAccessReviewer:  true,
		reviewSkillSecretsVerifier: true,
		reviewSkillPolicyRefresher: true,
	}
	if !known[name] {
		return fmt.Errorf("unknown review skill %q; run 'aegisclaw review list' to see available skills", name)
	}

	// Try the daemon API first.
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	client := api.NewClient(env.Config.Daemon.SocketPath)
	reqData, _ := json.Marshal(map[string]string{"name": name})
	resp, err := client.Call(cmd.Context(), "review.run", reqData)
	if err == nil && resp.Success {
		fmt.Printf("Review %q triggered. Worker ID: %s\n", name, extractWorkerID(resp.Data))
		return nil
	}

	// Fallback: run directly if daemon is not available.
	if err != nil {
		fmt.Printf("Daemon not available (%v); running review directly...\n", err)
	}

	return triggerReviewDirectly(cmd.Context(), env, name)
}

// triggerReviewDirectly runs a review skill without the daemon by spawning a
// worker directly in the current process.  Used when the daemon is offline.
func triggerReviewDirectly(ctx context.Context, env *runtimeEnv, name string) error {
	if env.Runtime == nil {
		return fmt.Errorf("firecracker runtime not available; start the daemon with 'aegisclaw start' and use 'aegisclaw review run %s'", name)
	}

	// Find the task description for the named skill.
	defs := buildReviewSkillDefs(env)
	var def *reviewSkillDef
	for i := range defs {
		if defs[i].Name == name {
			def = &defs[i]
			break
		}
	}
	if def == nil {
		return fmt.Errorf("no definition found for review skill %q", name)
	}

	fmt.Printf("Triggering review: %s\n", name)

	params := spawnWorkerParams{
		TaskDescription: def.TaskDescription,
		Role:            def.Role,
		TimeoutMins:     30,
		ToolsGranted:    []string{"list_audit_log", "list_skills", "store_memory", "retrieve_memory"},
		TaskID:          "review:" + name,
	}

	result, err := spawnWorker(ctx, env, params)
	if err != nil {
		return fmt.Errorf("review worker failed: %w", err)
	}

	// Write a signed audit entry for the manual trigger.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"skill_name":  name,
		"trigger":     "manual",
		"action":      "review_run",
		"result_preview": truncate(result, 200),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	act := kernel.NewAction(kernel.ActionSkillActivate, "operator", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(act); logErr != nil {
		env.Logger.Error("review run: failed to audit-log result", zap.Error(logErr))
	}

	fmt.Printf("Review %q completed.\n  Result preview: %s\n", name, truncate(result, 300))
	return nil
}

func runReviewDisable(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if !globalForce {
		fmt.Printf("Disable review skill %q? This will cancel its cron timer and log the action. [y/N] ", name)
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := disableReviewSkill(env, name); err != nil {
		return fmt.Errorf("disable review skill: %w", err)
	}

	fmt.Printf("Review skill %q disabled and action logged to audit trail.\n", name)
	return nil
}

func runReviewEnable(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	entry, ok := env.Registry.Get(name)
	if !ok {
		return fmt.Errorf("review skill %q not found; is the daemon running?", name)
	}

	meta := make(map[string]string, len(entry.Metadata))
	for k, v := range entry.Metadata {
		meta[k] = v
	}
	delete(meta, reviewMetadataKeyDisabled)

	if err := env.Registry.UpdateMetadata(name, meta); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}

	// Audit-log the enable action.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"skill_name": name,
		"action":     "review_skill_enabled",
		"operator":   "operator",
	})
	act := kernel.NewAction(kernel.ActionSkillActivate, "operator", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(act); logErr != nil {
		env.Logger.Error("review enable: failed to audit-log", zap.Error(logErr))
	}

	fmt.Printf("Review skill %q enabled. Restart the daemon to re-register its cron timer.\n", name)
	return nil
}

// extractWorkerID extracts the worker_id field from a JSON response.
func extractWorkerID(data []byte) string {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return "unknown"
	}
	if id, ok := m["worker_id"].(string); ok {
		return id
	}
	return "unknown"
}

// makeReviewRunHandler handles daemon-side `review.run` API calls.
// It immediately spawns an ephemeral worker for the named review skill.
func makeReviewRunHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, rawMsg json.RawMessage) *api.Response {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rawMsg, &req); err != nil || req.Name == "" {
			return &api.Response{Success: false, Error: "review name is required"}
		}

		known := map[string]bool{
			reviewSkillSecurityAuditor: true,
			reviewSkillAccessReviewer:  true,
			reviewSkillSecretsVerifier: true,
			reviewSkillPolicyRefresher: true,
		}
		if !known[req.Name] {
			return &api.Response{
				Success: false,
				Error:   fmt.Sprintf("unknown review skill: %q", req.Name),
			}
		}

		defs := buildReviewSkillDefs(env)
		var def *reviewSkillDef
		for i := range defs {
			if defs[i].Name == req.Name {
				def = &defs[i]
				break
			}
		}
		if def == nil {
			return &api.Response{Success: false, Error: "review skill definition not found"}
		}

		params := spawnWorkerParams{
			TaskDescription: def.TaskDescription,
			Role:            def.Role,
			TimeoutMins:     30,
			ToolsGranted:    []string{"list_audit_log", "list_skills", "store_memory", "retrieve_memory"},
			TaskID:          "review:" + req.Name,
		}

		// Spawn in a goroutine to avoid blocking the API call.
		workerID := generateVMID("review")
		go func() {
			result, err := spawnWorker(context.Background(), env, params)
			if err != nil {
				env.Logger.Error("review run: worker failed",
					zap.String("skill", req.Name),
					zap.String("worker_id", workerID),
					zap.Error(err),
				)
				return
			}
			env.Logger.Info("review run: worker completed",
				zap.String("skill", req.Name),
				zap.String("worker_id", workerID),
				zap.String("result_preview", truncate(result, 200)),
			)
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"skill_name":     req.Name,
				"worker_id":      workerID,
				"trigger":        "manual",
				"action":         "review_run",
				"result_preview": truncate(result, 200),
				"timestamp":      time.Now().UTC().Format(time.RFC3339),
			})
			act := kernel.NewAction(kernel.ActionSkillActivate, "operator", auditPayload)
			if _, logErr := env.Kernel.SignAndLog(act); logErr != nil {
				env.Logger.Error("review run: failed to audit-log result", zap.Error(logErr))
			}
		}()

		data, _ := json.Marshal(map[string]string{
			"worker_id": workerID,
			"skill":     req.Name,
			"status":    "spawning",
		})
		return &api.Response{Success: true, Data: data}
	}
}
