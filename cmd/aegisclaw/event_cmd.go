package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/spf13/cobra"
)

// eventCmd is the top-level `aegisclaw event` command group.
var eventCmd = &cobra.Command{
	Use:   "event",
	Short: "Manage async events: timers, signals, and approvals",
	Long: `Commands for managing the AegisClaw Event Bus (Phase 2).

  event timers              List active timers
  event signals             List received signals
  event approvals list      List pending (or all) approval requests
  event approvals approve   Approve a pending request
  event approvals reject    Reject a pending request
`,
}

var eventTimersCmd = &cobra.Command{
	Use:   "timers",
	Short: "List event bus timers",
	Long:  `Lists all active (or all) timers in the Event Bus.`,
	RunE:  runEventTimers,
}

var eventSignalsCmd = &cobra.Command{
	Use:   "signals",
	Short: "List received signals",
	Long:  `Lists recently received signals (from timers, webhooks, or external sources).`,
	RunE:  runEventSignals,
}

var eventApprovalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "Manage human approval requests",
	Long:  `List, approve, or reject pending human approval requests raised by agents.`,
}

var eventApprovalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List approval requests",
	RunE:  runEventApprovalsList,
}

var eventApprovalsApproveCmd = &cobra.Command{
	Use:   "approve <approval-id>",
	Short: "Approve a pending request",
	Args:  cobra.ExactArgs(1),
	RunE:  runEventApprovalsDecide,
}

var eventApprovalsRejectCmd = &cobra.Command{
	Use:   "reject <approval-id>",
	Short: "Reject a pending request",
	Args:  cobra.ExactArgs(1),
	RunE:  runEventApprovalsDecide,
}

var eventTimerStatusFlag string
var eventTaskIDFlag string
var eventAllFlag bool
var eventReasonFlag string
var eventLimitFlag int

func init() {
	eventTimersCmd.Flags().StringVar(&eventTimerStatusFlag, "status", "active", "Filter by status (active, fired, cancelled, expired, or empty for all)")
	eventSignalsCmd.Flags().StringVar(&eventTaskIDFlag, "task-id", "", "Filter by task ID")
	eventSignalsCmd.Flags().IntVar(&eventLimitFlag, "limit", 20, "Maximum number of signals to return")
	eventApprovalsListCmd.Flags().BoolVar(&eventAllFlag, "all", false, "Show all approvals (not just pending)")
	eventApprovalsApproveCmd.Flags().StringVar(&eventReasonFlag, "reason", "", "Reason for the decision")
	eventApprovalsRejectCmd.Flags().StringVar(&eventReasonFlag, "reason", "", "Reason for the decision")
}

// callEventAPI is a helper that calls a daemon event endpoint and prints output.
func callEventAPI(ctx context.Context, env *runtimeEnv, action string, req interface{}) (json.RawMessage, error) {
	client := api.NewClient(env.Config.Daemon.SocketPath)
	var reqData json.RawMessage
	if req != nil {
		var err error
		reqData, err = json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}
	resp, err := client.Call(ctx, action, reqData)
	if err != nil {
		return nil, fmt.Errorf("daemon unavailable (is `aegisclaw start` running?): %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s: %s", action, resp.Error)
	}
	return resp.Data, nil
}

func runEventTimers(cmd *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	data, err := callEventAPI(cmd.Context(), env, "event.timers.list", map[string]string{
		"status": eventTimerStatusFlag,
	})
	if err != nil {
		return err
	}

	var timers []*eventbus.Timer
	if err := json.Unmarshal(data, &timers); err != nil {
		fmt.Println(string(data))
		return nil
	}

	if len(timers) == 0 {
		fmt.Println("No timers found.")
		return nil
	}

	fmt.Printf("Timers (%d):\n", len(timers))
	for _, t := range timers {
		next := "N/A"
		if t.NextFireAt != nil {
			next = t.NextFireAt.Format(time.RFC3339)
		} else if t.TriggerAt != nil {
			next = t.TriggerAt.Format(time.RFC3339)
		}
		fmt.Printf("  [%s]  %-24s  %-10s  next=%-25s  task=%s\n",
			t.TimerID, t.Name, t.Status, next, t.TaskID)
	}
	return nil
}

func runEventSignals(cmd *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	data, err := callEventAPI(cmd.Context(), env, "event.signals.list", map[string]interface{}{
		"task_id": eventTaskIDFlag,
		"limit":   eventLimitFlag,
	})
	if err != nil {
		return err
	}

	var signals []*eventbus.Signal
	if err := json.Unmarshal(data, &signals); err != nil {
		fmt.Println(string(data))
		return nil
	}

	if len(signals) == 0 {
		fmt.Println("No signals found.")
		return nil
	}

	fmt.Printf("Signals (%d, newest first):\n", len(signals))
	for _, s := range signals {
		desc := ""
		if len(s.Payload) > 0 && string(s.Payload) != "null" {
			desc = "  payload=" + truncate(string(s.Payload), 60)
		}
		fmt.Printf("  [%s]  %-10s  type=%-8s  task=%-12s  at=%s%s\n",
			s.SignalID, s.Source, s.Type, s.TaskID,
			s.ReceivedAt.Format(time.RFC3339), desc)
	}
	return nil
}

func runEventApprovalsList(cmd *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	data, err := callEventAPI(cmd.Context(), env, "event.approvals.list", map[string]bool{
		"pending_only": !eventAllFlag,
	})
	if err != nil {
		return err
	}

	var approvals []*eventbus.ApprovalRequest
	if err := json.Unmarshal(data, &approvals); err != nil {
		fmt.Println(string(data))
		return nil
	}

	if len(approvals) == 0 {
		if eventAllFlag {
			fmt.Println("No approval requests found.")
		} else {
			fmt.Println("No pending approvals. (Use --all to see all.)")
		}
		return nil
	}

	fmt.Printf("Approvals (%d):\n", len(approvals))
	for _, a := range approvals {
		decided := ""
		if a.DecidedAt != nil {
			decided = fmt.Sprintf("  decided=%s by=%s", a.DecidedAt.Format(time.RFC3339), a.DecidedBy)
		}
		fmt.Printf("  [%s]  %-8s  risk=%-6s  %s\n    %s%s\n",
			a.ApprovalID, a.Status, a.RiskLevel,
			a.CreatedAt.Format(time.RFC3339),
			truncate(a.Title, 60), decided)
		if a.Description != "" {
			fmt.Printf("    %s\n", truncate(a.Description, 120))
		}
	}
	return nil
}

func runEventApprovalsDecide(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	approved := cmd.Use == "approve <approval-id>"
	approvalID := args[0]

	decision := "reject"
	if approved {
		decision = "approve"
	}

	if !globalForce {
		fmt.Printf("About to %s approval request %q.\n", decision, approvalID)
		if eventReasonFlag != "" {
			fmt.Printf("Reason: %s\n", eventReasonFlag)
		}
		fmt.Println("Use --force to confirm.")
		return nil
	}

	data, err := callEventAPI(cmd.Context(), env, "event.approvals.decide", map[string]interface{}{
		"approval_id": approvalID,
		"approved":    approved,
		"decided_by":  "user",
		"reason":      eventReasonFlag,
	})
	if err != nil {
		return err
	}

	var result struct {
		ApprovalID string `json:"approval_id"`
		Decision   string `json:"decision"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Println(string(data))
		return nil
	}

	verb := strings.ToUpper(result.Decision[:1]) + result.Decision[1:]
	fmt.Printf("%s: Approval request %s has been %s.\n", verb, result.ApprovalID, result.Decision)
	return nil
}
