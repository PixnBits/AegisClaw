package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "v0.1.0"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "aegisclaw",
	Short: "AegisClaw - Paranoid Firecracker-isolated agent platform",
	Long: `AegisClaw is a security-first platform for running isolated agents in Firecracker microVMs.
All operations are signed, logged, and subject to governance court review.`,
}

// sandboxCmd represents the sandbox command
var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Manage AegisClaw sandboxes",
	Long:  `Commands for listing, starting, stopping, and deleting Firecracker sandboxes.`,
}

// lsCmd represents the ls command
var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all AegisClaw sandboxes",
	Long:  `Displays a list of all running and stopped AegisClaw sandboxes with their status.`,
	RunE:  runLs,
}

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the AegisClaw kernel",
	Long: `Starts the AegisClaw kernel singleton, loads configuration,
and initializes the message-hub skill in its own microVM.
All subsequent operations require the kernel to be running.`,
	RunE: runStart,
}

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show AegisClaw kernel and sandbox status",
	Long:  `Displays the current status of the kernel, running sandboxes, and system health.`,
	RunE:  runStatus,
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show AegisClaw version",
	Long:  `Displays the current version of AegisClaw.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("AegisClaw %s\n", version)
	},
}

// sandboxStartCmd represents the sandbox start command
var sandboxStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start an AegisClaw sandbox",
	Long:  `Starts a new Firecracker microVM sandbox with the specified name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSandboxStart,
}

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop an AegisClaw sandbox",
	Long:  `Stops the running Firecracker microVM sandbox with the specified name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSandboxStop,
}

// deleteCmd represents the delete command
var deleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an AegisClaw sandbox",
	Long:  `Permanently deletes the Firecracker microVM sandbox with the specified name and its resources.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSandboxDelete,
}

// auditCmd represents the audit command group
var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit log operations",
	Long:  `Commands for verifying the tamper-evident Merkle audit chain.`,
}

// auditVerifyCmd verifies the integrity of the Merkle audit chain
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
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(sandboxCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(proposeCmd)
	rootCmd.AddCommand(courtCmd)
	rootCmd.AddCommand(builderCmd)
	rootCmd.AddCommand(secretCmd)

	sandboxCmd.AddCommand(lsCmd)
	sandboxCmd.AddCommand(sandboxStartCmd)
	sandboxCmd.AddCommand(stopCmd)
	sandboxCmd.AddCommand(deleteCmd)

	skillCmd.AddCommand(skillActivateCmd)
	skillCmd.AddCommand(skillDeactivateCmd)
	skillCmd.AddCommand(skillListCmd)

	auditCmd.AddCommand(auditVerifyCmd)
	auditCmd.AddCommand(auditExplorerCmd)

	proposeCmd.AddCommand(proposeSkillCmd)
	proposeCmd.AddCommand(proposeSubmitCmd)
	proposeCmd.AddCommand(proposeListCmd)
	proposeCmd.AddCommand(proposeShowCmd)
	proposeCmd.Flags().StringVar(&proposeCategory, "category", "new_skill", "Proposal category (new_skill, edit_skill, delete_skill, kernel_patch, config_change)")

	courtCmd.AddCommand(courtReviewCmd)
	courtCmd.AddCommand(courtVoteCmd)
	courtCmd.AddCommand(courtSessionsCmd)
	courtCmd.AddCommand(courtDashboardCmd)

	statusCmd.Flags().BoolVar(&statusTUI, "tui", false, "Launch interactive TUI dashboard")

	secretCmd.AddCommand(secretAddCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretDeleteCmd)
	secretAddCmd.Flags().StringVar(&secretSkillID, "skill", "", "Skill ID to associate with the secret")
	secretAddCmd.MarkFlagRequired("skill")
}
