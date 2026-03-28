package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/vault"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

var secretsSkillID string

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage secrets (add, list, rotate) — never exposes values",
	Long: `Commands for adding, listing, and rotating age-encrypted secrets.
Secret values are never stored in plaintext, never echoed, and never
available via chat. Secrets are only managed through these CLI commands.`,
}

var secretsAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add or update a secret (secure prompt, never echoes)",
	Long: `Adds or updates a secret using a secure interactive prompt.
The secret value is never echoed to the terminal, never logged,
and never available through chat or any non-CLI interface.

The secret is associated with a skill via the --skill flag.`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretsAdd,
}

var secretsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored secrets (names only, never values)",
	Long:  `Displays metadata for all secrets in the vault. Secret values are never shown.`,
	RunE:  runSecretsList,
}

var secretsRotateCmd = &cobra.Command{
	Use:   "rotate <name>",
	Short: "Rotate a secret value",
	Long: `Prompts for a new value for an existing secret.
The old value is overwritten and the rotation is logged to the audit trail.`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretsRotate,
}

func init() {
	secretsCmd.AddCommand(secretsAddCmd)
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsRotateCmd)

	secretsAddCmd.Flags().StringVar(&secretsSkillID, "skill", "", "Skill name to associate with the secret")
	secretsAddCmd.MarkFlagRequired("skill")

	secretsRotateCmd.Flags().StringVar(&secretsSkillID, "skill", "", "Skill name to associate with the rotated secret")
}

// readSecretFromTerminal reads a secret from terminal without echoing.
func readSecretFromTerminal(prompt string) (string, error) {
	fmt.Print(prompt)

	// Try to disable terminal echo for secure input.
	fd := int(os.Stdin.Fd())
	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err == nil {
		newState := *oldState
		newState.Lflag &^= unix.ECHO
		unix.IoctlSetTermios(fd, unix.TCSETS, &newState)
		defer func() {
			unix.IoctlSetTermios(fd, unix.TCSETS, oldState)
			fmt.Println() // newline after hidden input
		}()
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimRight(line, "\n\r"), nil
}

func runSecretsAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	if secretsSkillID == "" {
		return fmt.Errorf("--skill flag is required")
	}

	// Read secret from secure prompt (never as CLI argument per PRD).
	value, err := readSecretFromTerminal("Enter secret value: ")
	if err != nil {
		return fmt.Errorf("failed to read secret: %w", err)
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	env, err := initRuntime()
	if err != nil {
		return err
	}

	v, err := vault.NewVault(env.Config.Vault.Dir, env.Kernel.PrivateKeyBytes(), env.Logger)
	if err != nil {
		return fmt.Errorf("failed to open vault: %w", err)
	}

	if err := v.Add(name, secretsSkillID, []byte(value)); err != nil {
		return fmt.Errorf("failed to add secret: %w", err)
	}

	// Audit log the secret addition (value is never logged).
	auditPayload := fmt.Appendf(nil, `{"name":%q,"skill_id":%q}`, name, secretsSkillID)
	action := kernel.NewAction(kernel.ActionSecretAdd, "cli", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit log secret add", zap.Error(logErr))
	}

	fmt.Printf("Secret %q stored for skill %q\n", name, secretsSkillID)
	return nil
}

func runSecretsList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}

	v, err := vault.NewVault(env.Config.Vault.Dir, env.Kernel.PrivateKeyBytes(), env.Logger)
	if err != nil {
		return fmt.Errorf("failed to open vault: %w", err)
	}

	entries := v.List()
	if len(entries) == 0 {
		fmt.Println("No secrets stored.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSKILL\tCREATED\tUPDATED")
	fmt.Fprintln(w, "----\t-----\t-------\t-------")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			e.Name,
			e.SkillID,
			e.CreatedAt.Format("2006-01-02 15:04"),
			e.UpdatedAt.Format("2006-01-02 15:04"),
		)
	}
	w.Flush()

	return nil
}

func runSecretsRotate(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Read new secret from secure prompt.
	value, err := readSecretFromTerminal("Enter new secret value: ")
	if err != nil {
		return fmt.Errorf("failed to read secret: %w", err)
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	env, err := initRuntime()
	if err != nil {
		return err
	}

	v, err := vault.NewVault(env.Config.Vault.Dir, env.Kernel.PrivateKeyBytes(), env.Logger)
	if err != nil {
		return fmt.Errorf("failed to open vault: %w", err)
	}

	// Determine skill ID: use flag or keep existing association.
	skillID := secretsSkillID
	if skillID == "" {
		// Try to keep existing skill association.
		for _, e := range v.List() {
			if e.Name == name {
				skillID = e.SkillID
				break
			}
		}
	}
	if skillID == "" {
		return fmt.Errorf("secret %q not found (use secrets add to create it)", name)
	}

	if err := v.Add(name, skillID, []byte(value)); err != nil {
		return fmt.Errorf("failed to rotate secret: %w", err)
	}

	// Audit log the rotation (value is never logged).
	auditPayload := fmt.Appendf(nil, `{"name":%q,"skill_id":%q,"action":"rotate"}`, name, skillID)
	action := kernel.NewAction(kernel.ActionSecretAdd, "cli", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit log secret rotation", zap.Error(logErr))
	}

	fmt.Printf("Secret %q rotated for skill %q\n", name, skillID)
	return nil
}
