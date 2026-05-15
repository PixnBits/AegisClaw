package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/spf13/cobra"
)

// registerCLISpecCommands wires CLI verbs from docs/specs/cli.md and
// docs/implementation-plan/01-cli-full-coverage.md. Commands are thin
// clients over the local Unix socket API except where noted.
func registerCLISpecCommands() {
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(tasksCmd)
	rootCmd.AddCommand(teamCmd)
	rootCmd.AddCommand(courtCmd)
	rootCmd.AddCommand(autonomyCmd)
	rootCmd.AddCommand(completionCmd)

	skillCmd.Aliases = []string{"skills"}

	skillCmd.AddCommand(skillStatusCmd)
}

var restartCmd = &cobra.Command{
	Use:   "restart [component]",
	Short: "Restart the coordinator daemon",
	Long: `Stops and starts the Host Daemon via the control socket and start command.
Optional component is accepted for forward compatibility (only "daemon"
and "coordinator" are recognised today).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestart,
}

func runRestart(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		c := strings.ToLower(strings.TrimSpace(args[0]))
		if c != "" && c != "daemon" && c != "coordinator" {
			return fmt.Errorf("unknown component %q (supported: daemon, coordinator)", args[0])
		}
	}
	client := api.NewClient(resolveDaemonSocketPath())
	resp, err := client.Call(cmd.Context(), "kernel.shutdown", nil)
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w\n(Is the daemon running?)", err)
	}
	if !resp.Success {
		return fmt.Errorf("restart (shutdown) failed: %s", resp.Error)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	startProc := exec.Command(exePath, "start")
	if err := startProc.Start(); err != nil {
		return fmt.Errorf("restart failed to start daemon: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if pingErr := client.Ping(cmd.Context()); pingErr == nil {
			fmt.Println("Daemon restarted.")
			return nil
		}
	}
	return fmt.Errorf("daemon start command launched (pid %d) but daemon did not become reachable in time", startProc.Process.Pid)
}

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List and control chat sessions",
	Long:  `Inspect session state and pause, resume, or close sessions (docs/specs/cli.md).`,
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked sessions",
	RunE:  runSessionsList,
}

var sessionsStatusCmd = &cobra.Command{
	Use:   "status <session-id>",
	Short: "Show status for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsStatus,
}

var sessionsCancelCmd = &cobra.Command{
	Use:   "cancel <session-id>",
	Short: "Mark a session as closed (same intent as sessions kill)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsCancel,
}

var sessionsPauseCmd = &cobra.Command{
	Use:   "pause <session-id>",
	Short: "Pause a session (blocks sends until resumed)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsPause,
}

var sessionsResumeCmd = &cobra.Command{
	Use:   "resume <session-id>",
	Short: "Resume a paused session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsResume,
}

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Inspect async tasks (worker-backed)",
	Long:  `Tasks map to worker records: list, status, pause, resume, cancel.`,
}

var tasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks / workers",
	RunE:  runTasksList,
}

var tasksActiveOnly bool

var tasksStatusCmd = &cobra.Command{
	Use:   "status <task-id>",
	Short: "Show status for a task (matches worker task_id or worker_id)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksStatus,
}

var tasksPauseCmd = &cobra.Command{
	Use:   "pause <task-id>",
	Short: "Pause a running task (not supported for workers yet)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksPause,
}

var tasksResumeCmd = &cobra.Command{
	Use:   "resume <task-id>",
	Short: "Resume a paused task (not supported for workers yet)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksResume,
}

var tasksCancelCmd = &cobra.Command{
	Use:   "cancel <task-id>",
	Short: "Cancel a task (not supported for workers yet)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksCancel,
}

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "Multi-agent teams (metadata registry)",
	Long:  `Lightweight team registry stored alongside audit state (0700).`,
}

var teamListCmd = &cobra.Command{
	Use:   "list",
	Short: "List teams",
	RunE:  runTeamList,
}

var teamCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a team",
	Args:  cobra.ExactArgs(1),
	RunE:  runTeamCreate,
}

var teamJoinCmd = &cobra.Command{
	Use:   "join <team-id> <member>",
	Short: "Add a member to a team",
	Args:  cobra.ExactArgs(2),
	RunE:  runTeamJoin,
}

var teamLeaveCmd = &cobra.Command{
	Use:   "leave <team-id> <member>",
	Short: "Remove a member from a team",
	Args:  cobra.ExactArgs(2),
	RunE:  runTeamLeave,
}

var teamStatusCmd = &cobra.Command{
	Use:   "status <team-id>",
	Short: "Show team details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTeamStatus,
}

var courtCmd = &cobra.Command{
	Use:   "court",
	Short: "Governance court operations",
}

var courtDecisionsCmd = &cobra.Command{
	Use:   "decisions",
	Short: "Court decisions",
}

var courtDecisionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List court decision sessions",
	RunE:  runCourtDecisionsList,
}

var courtDecisionsShowCmd = &cobra.Command{
	Use:   "show <decision-id>",
	Short: "Show one court decision session",
	Args:  cobra.ExactArgs(1),
	RunE:  runCourtDecisionsShow,
}

var autonomyCmd = &cobra.Command{
	Use:   "autonomy",
	Short: "Session autonomy grants",
	Long:  `Grant, revoke, reset, or inspect autonomy presets for a session id.`,
}

var autonomyShowCmd = &cobra.Command{
	Use:   "show <session-id>",
	Short: "Show autonomy state for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutonomyShow,
}

var autonomyGrantPreset string
var autonomyGrantDuration time.Duration

var autonomyGrantCmd = &cobra.Command{
	Use:   "grant <session-id>",
	Short: "Grant autonomy preset for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutonomyGrant,
}

var autonomyRevokeCmd = &cobra.Command{
	Use:   "revoke <session-id>",
	Short: "Revoke autonomy for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutonomyRevoke,
}

var autonomyResetCmd = &cobra.Command{
	Use:   "reset <session-id>",
	Short: "Reset autonomy state for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutonomyReset,
}

var skillStatusCmd = &cobra.Command{
	Use:   "status [skill-name]",
	Short: "Show live skill status from the daemon",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSkillStatus,
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Write completion scripts to stdout. Example:

  source <(aegisclaw completion bash)

Never log or store generated scripts in world-readable locations.`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactValidArgs(1),
	RunE:                  runCompletion,
}

func init() {
	registerCLISpecCommands()

	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsStatusCmd)
	sessionsCmd.AddCommand(sessionsCancelCmd)
	sessionsCmd.AddCommand(sessionsPauseCmd)
	sessionsCmd.AddCommand(sessionsResumeCmd)

	tasksCmd.AddCommand(tasksListCmd)
	tasksCmd.AddCommand(tasksStatusCmd)
	tasksCmd.AddCommand(tasksPauseCmd)
	tasksCmd.AddCommand(tasksResumeCmd)
	tasksCmd.AddCommand(tasksCancelCmd)
	tasksListCmd.Flags().BoolVar(&tasksActiveOnly, "active", false, "Only active workers")

	teamCmd.AddCommand(teamListCmd)
	teamCmd.AddCommand(teamCreateCmd)
	teamCmd.AddCommand(teamJoinCmd)
	teamCmd.AddCommand(teamLeaveCmd)
	teamCmd.AddCommand(teamStatusCmd)

	courtCmd.AddCommand(courtDecisionsCmd)
	courtDecisionsCmd.AddCommand(courtDecisionsListCmd)
	courtDecisionsCmd.AddCommand(courtDecisionsShowCmd)

	autonomyCmd.AddCommand(autonomyShowCmd)
	autonomyCmd.AddCommand(autonomyGrantCmd)
	autonomyCmd.AddCommand(autonomyRevokeCmd)
	autonomyCmd.AddCommand(autonomyResetCmd)
	autonomyGrantCmd.Flags().StringVar(&autonomyGrantPreset, "preset", "default", "Named autonomy preset")
	autonomyGrantCmd.Flags().DurationVar(&autonomyGrantDuration, "duration", 0, "Optional time-bound grant (e.g. 30m)")
}

func runCompletion(cmd *cobra.Command, args []string) error {
	switch args[0] {
	case "bash":
		return cmd.Root().GenBashCompletionV2(os.Stdout, true)
	case "zsh":
		return cmd.Root().GenZshCompletion(os.Stdout)
	case "fish":
		return cmd.Root().GenFishCompletion(os.Stdout, true)
	case "powershell":
		return cmd.Root().GenPowerShellCompletion(os.Stdout)
	default:
		return fmt.Errorf("unsupported shell %q", args[0])
	}
}

func daemonCall(ctx context.Context, action string, req any) (*api.Response, error) {
	client := api.NewClient(resolveDaemonSocketPath())
	var raw json.RawMessage
	var err error
	if req != nil {
		raw, err = json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}
	return client.Call(ctx, action, raw)
}

func runSessionsList(cmd *cobra.Command, _ []string) error {
	resp, err := daemonCall(cmd.Context(), "sessions.list", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("sessions.list: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	var parsed struct {
		Sessions []sessionSummary `json:"sessions"`
		Total    int              `json:"total"`
	}
	if err := json.Unmarshal(resp.Data, &parsed); err != nil {
		return err
	}
	if len(parsed.Sessions) == 0 {
		fmt.Println("No sessions.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tMESSAGES\tSANDBOX")
	for _, s := range parsed.Sessions {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", s.ID, s.Status, s.MessageCount, s.SandboxID)
	}
	w.Flush()
	return nil
}

func runSessionsStatus(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "sessions.status", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("sessions.status: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	var m map[string]interface{}
	_ = json.Unmarshal(resp.Data, &m)
	fmt.Printf("Session %v\n  status: %v\n  messages: %v\n  sandbox: %v\n", m["session_id"], m["status"], m["message_count"], m["sandbox_id"])
	return nil
}

func runSessionsCancel(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "sessions.cancel", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("sessions.cancel: %s", resp.Error)
	}
	fmt.Println("Session closed.")
	return nil
}

func runSessionsPause(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "sessions.pause", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("sessions.pause: %s", resp.Error)
	}
	fmt.Println("Session paused.")
	return nil
}

func runSessionsResume(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "sessions.resume", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("sessions.resume: %s", resp.Error)
	}
	fmt.Println("Session resumed.")
	return nil
}

func runTasksList(cmd *cobra.Command, _ []string) error {
	resp, err := daemonCall(cmd.Context(), "tasks.list", map[string]bool{"active_only": tasksActiveOnly})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("tasks.list: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(resp.Data, &rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("No tasks.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TASK_ID\tWORKER_ID\tSTATUS\tROLE")
	for _, r := range rows {
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", r["task_id"], r["worker_id"], r["status"], r["role"])
	}
	w.Flush()
	return nil
}

func runTasksStatus(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "tasks.status", map[string]string{"task_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("tasks.status: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	fmt.Println(string(resp.Data))
	return nil
}

func runTasksPause(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "tasks.pause", map[string]string{"task_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("tasks.pause: %s", resp.Error)
	}
	fmt.Println("Task pause requested.")
	return nil
}

func runTasksResume(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "tasks.resume", map[string]string{"task_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("tasks.resume: %s", resp.Error)
	}
	fmt.Println("Task resume requested.")
	return nil
}

func runTasksCancel(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "tasks.cancel", map[string]string{"task_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("tasks.cancel: %s", resp.Error)
	}
	fmt.Println("Task cancel requested.")
	return nil
}

func runTeamList(cmd *cobra.Command, _ []string) error {
	resp, err := daemonCall(cmd.Context(), "team.list", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("team.list: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	var teams []teamRecord
	if err := json.Unmarshal(resp.Data, &teams); err != nil {
		return err
	}
	if len(teams) == 0 {
		fmt.Println("No teams.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tMEMBERS")
	for _, t := range teams {
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Name, strings.Join(t.Members, ","))
	}
	w.Flush()
	return nil
}

func runTeamCreate(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "team.create", map[string]string{"name": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("team.create: %s", resp.Error)
	}
	fmt.Println(string(resp.Data))
	return nil
}

func runTeamJoin(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "team.join", map[string]string{"team_id": args[0], "member": args[1]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("team.join: %s", resp.Error)
	}
	fmt.Println("Joined team.")
	return nil
}

func runTeamLeave(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "team.leave", map[string]string{"team_id": args[0], "member": args[1]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("team.leave: %s", resp.Error)
	}
	fmt.Println("Left team.")
	return nil
}

func runTeamStatus(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "team.status", map[string]string{"team_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("team.status: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	fmt.Println(string(resp.Data))
	return nil
}

func runCourtDecisionsList(cmd *cobra.Command, _ []string) error {
	resp, err := daemonCall(cmd.Context(), "court.decisions.list", map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("court.decisions.list: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(resp.Data, &rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("No court sessions.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPROPOSAL\tSTATE\tVERDICT")
	for _, r := range rows {
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", r["id"], r["proposal_id"], r["state"], r["verdict"])
	}
	w.Flush()
	return nil
}

func runCourtDecisionsShow(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "court.decisions.show", map[string]string{"id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("court.decisions.show: %s", resp.Error)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, resp.Data, "", "  "); err != nil {
		fmt.Println(string(resp.Data))
		return nil
	}
	fmt.Println(buf.String())
	return nil
}

func runAutonomyShow(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "autonomy.show", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("autonomy.show: %s", resp.Error)
	}
	fmt.Println(string(resp.Data))
	return nil
}

func runAutonomyGrant(cmd *cobra.Command, args []string) error {
	req := map[string]string{
		"session_id": args[0],
		"preset":     autonomyGrantPreset,
	}
	if autonomyGrantDuration > 0 {
		req["duration"] = autonomyGrantDuration.String()
	}
	resp, err := daemonCall(cmd.Context(), "autonomy.grant", req)
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("autonomy.grant: %s", resp.Error)
	}
	fmt.Println("Autonomy granted.")
	return nil
}

func runAutonomyRevoke(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "autonomy.revoke", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("autonomy.revoke: %s", resp.Error)
	}
	fmt.Println("Autonomy revoked.")
	return nil
}

func runAutonomyReset(cmd *cobra.Command, args []string) error {
	resp, err := daemonCall(cmd.Context(), "autonomy.reset", map[string]string{"session_id": args[0]})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("autonomy.reset: %s", resp.Error)
	}
	fmt.Println("Autonomy reset.")
	return nil
}

func runSkillStatus(cmd *cobra.Command, args []string) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		// When omitted, list all skills (status overview).
		resp, err := daemonCall(cmd.Context(), "skill.list", nil)
		if err != nil {
			return fmt.Errorf("daemon: %w", err)
		}
		if !resp.Success {
			return fmt.Errorf("skill.list: %s", resp.Error)
		}
		if globalJSON {
			fmt.Println(string(resp.Data))
			return nil
		}
		fmt.Println(string(resp.Data))
		return nil
	}
	resp, err := daemonCall(cmd.Context(), "skill.status", map[string]string{"name": name})
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("skill.status: %s", resp.Error)
	}
	if globalJSON {
		fmt.Println(string(resp.Data))
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, resp.Data, "", "  "); err != nil {
		fmt.Println(string(resp.Data))
		return nil
	}
	fmt.Println(buf.String())
	return nil
}
