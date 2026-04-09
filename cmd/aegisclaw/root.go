package main

import (
	"github.com/spf13/cobra"
)

const version = "v0.1.0"

// rootCmd represents the base command when called without any subcommands.
// The CLI surface matches the published specification in docs/cli-design.md:
//
//	Core Commands:
//	  init, start, stop, status, chat, skill, audit, secrets, self, version
//
//	Global Flags:
//	  --json, --verbose/-v, --dry-run, --force
var rootCmd = &cobra.Command{
	SilenceErrors: true,
	Use:           "aegisclaw",
	Short:         "AegisClaw - Paranoid Firecracker-isolated agent platform",
	Long: `AegisClaw is a security-first platform for running isolated agents in Firecracker microVMs.
All operations are signed, logged, and subject to governance court review.

Get started:
  aegisclaw init          One-time setup
  aegisclaw start         Start the coordinator daemon
  aegisclaw chat          Enter interactive chat with the main agent

Security reminders:
  • Secrets are ONLY managed via 'aegisclaw secrets' — never via chat.
  • High-risk actions always require confirmation unless --force is used.
  • Everything is recorded in the append-only Merkle-tree audit log.`,
}

// startCmd represents the start command.
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the coordinator daemon",
	Long: `Starts the MicroVM Coordinator Daemon, provisions Firecracker assets,
initializes the message-hub and IPC bridge, starts the Unix socket API
server, and blocks until interrupted (Ctrl+C or 'aegisclaw stop').

Use --safe to enter Safe Mode: deactivates all skills and blocks skill
activation/invocation. No Court, no main agent sandbox, no LLM interaction.`,
	SilenceUsage: true,
	RunE:         runStart,
}

// statusCmd represents the status command.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status and health",
	Long: `Displays version, public key, sandbox counts, skill counts, registry root
hash, and audit chain summary.

Use --tui to launch an interactive TUI dashboard with live updates.
Supports --json for scripting.`,
	RunE: runStatus,
}

// versionCmd represents the version command.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and build information",
	Long:  `Displays version, git commit, build date, Go version, and OS/architecture.`,
	Run:   runVersion,
}

// skillCmd is the skill management command group.
// Subcommands: add, list, revoke, info
var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skills (add, list, revoke, info)",
	Long: `Commands for managing AegisClaw skills. Each skill runs in its
own Firecracker microVM with enforced isolation boundaries.

  skill add     Propose and add a new skill (triggers Court review)
  skill list    List registered skills and their status
  skill revoke  Revoke and remove a skill
  skill info    Show detailed information about a skill`,
}

// Skill subcommands.
var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered skills",
	Long:  `Displays all registered skills with their state, version, and sandbox information.`,
	RunE:  runSkillList,
}

var skillRevokeCmd = &cobra.Command{
	Use:   "revoke <skill-name>",
	Short: "Revoke and remove a skill",
	Long: `Stops the skill's microVM, removes it from the registry, and logs
the revocation to the audit trail. Requires confirmation unless --force is used.`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillRevoke,
}

var skillInfoCmd = &cobra.Command{
	Use:   "info <skill-name>",
	Short: "Show detailed skill information",
	Long:  `Displays full details about a registered skill including its sandbox, version, and metadata.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillInfo,
}

// auditCmd represents the audit command group.
var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Query the append-only audit log",
	Long: `Commands for querying and verifying the tamper-evident Merkle audit chain.

  audit log     Browse audit log entries with filters
  audit why     Explain why an action was performed
  audit verify  Verify Merkle-tree integrity`,
}

// auditVerifyCmd verifies the integrity of the Merkle audit chain.
var auditVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the Merkle audit chain integrity",
	Long: `Reads the entire audit log and verifies:
  - Each entry's SHA-256 hash is correct for its contents
  - Each entry's prev_hash links to the previous entry's hash
  - Each entry's Ed25519 signature is valid`,
	RunE: runAuditVerify,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	// Global flags (CLI spec §2).
	rootCmd.PersistentFlags().BoolVar(&globalJSON, "json", false, "Output in structured JSON (for scripting)")
	rootCmd.PersistentFlags().BoolVarP(&globalVerbose, "verbose", "v", false, "Increase verbosity")
	rootCmd.PersistentFlags().BoolVar(&globalDryRun, "dry-run", false, "Simulate action without making changes")
	rootCmd.PersistentFlags().BoolVar(&globalForce, "force", false, "Skip confirmations (logged in audit trail)")

	// Core Commands (CLI spec §2): init, start, stop, status, chat, skill, audit, secrets, self, version, memory
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopDaemonCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(secretsCmd)
	rootCmd.AddCommand(selfCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(memoryCmd)
	rootCmd.AddCommand(eventCmd)
	rootCmd.AddCommand(workerCmd)
	rootCmd.AddCommand(evalCmd)
	rootCmd.AddCommand(reviewCmd)

	// skill subcommands: add, list, revoke, info, sbom
	skillCmd.AddCommand(skillAddCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillRevokeCmd)
	skillCmd.AddCommand(skillInfoCmd)
	skillCmd.AddCommand(skillSBOMCmd)

	// audit subcommands: log, why, verify
	auditCmd.AddCommand(auditLogCmd)
	auditCmd.AddCommand(auditWhyCmd)
	auditCmd.AddCommand(auditVerifyCmd)

	// memory subcommands: search, list, compact, delete
	memoryCmd.AddCommand(memorySearchCmd)
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryCompactCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)

	// event subcommands: timers, signals, approvals [list/approve/reject]
	eventCmd.AddCommand(eventTimersCmd)
	eventCmd.AddCommand(eventSignalsCmd)
	eventCmd.AddCommand(eventApprovalsCmd)
	eventApprovalsCmd.AddCommand(eventApprovalsListCmd)
	eventApprovalsCmd.AddCommand(eventApprovalsApproveCmd)
	eventApprovalsCmd.AddCommand(eventApprovalsRejectCmd)

	// worker subcommands: list, status
	workerCmd.AddCommand(workerListCmd)
	workerCmd.AddCommand(workerStatusCmd)
	// eval subcommands: run, report
	evalCmd.AddCommand(evalRunCmd)
	evalCmd.AddCommand(evalReportCmd)

	// review subcommands: list, run, disable, enable
	reviewCmd.AddCommand(reviewListCmd)
	reviewCmd.AddCommand(reviewRunCmd)
	reviewCmd.AddCommand(reviewDisableCmd)
	reviewCmd.AddCommand(reviewEnableCmd)

	// start flags
	statusCmd.Flags().BoolVar(&statusTUI, "tui", false, "Launch interactive TUI dashboard")
	startCmd.Flags().BoolVar(&safeModeFlag, "safe", false, "Start in Safe Mode: minimal recovery environment, no skills, no Court, no LLM")
	startCmd.Flags().StringVar(&startModelFlag, "model", "", "Override the default LLM model for this session (must be in the registry)")
}
