package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/spf13/cobra"
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

var secretsRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Re-inject secrets into a running skill VM after rotation",
	Long: `Pushes updated vault secrets to an already-active skill VM via vsock
without a full deactivate/activate cycle.  Run this after "secrets rotate"
to make the new value available to the running skill immediately.`,
	RunE: runSecretsRefresh,
}

func init() {
	secretsCmd.AddCommand(secretsAddCmd)
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsRotateCmd)
	secretsCmd.AddCommand(secretsRefreshCmd)

	secretsAddCmd.Flags().StringVar(&secretsSkillID, "skill", "", "Skill name to associate with the secret")
	secretsAddCmd.MarkFlagRequired("skill")

	secretsRotateCmd.Flags().StringVar(&secretsSkillID, "skill", "", "Skill name to associate with the rotated secret")

	secretsRefreshCmd.Flags().StringVar(&secretsSkillID, "skill", "", "Skill name whose running VM should receive refreshed secrets")
	secretsRefreshCmd.MarkFlagRequired("skill")
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

	// Read secret from secure prompt before contacting the daemon so the
	// terminal interaction is clean and the plaintext is never in a shell arg.
	value, err := readSecretFromTerminal("Enter secret value: ")
	if err != nil {
		return fmt.Errorf("failed to read secret: %w", err)
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	client := api.NewClient(resolveDaemonSocketPath())
	resp, err := client.Call(cmd.Context(), "vault.secret.add", api.VaultSecretAddRequest{
		Name:    name,
		SkillID: secretsSkillID,
		Value:   value,
	})
	if err != nil {
		return fmt.Errorf("daemon call failed: %w\n  (Is the daemon running? Start with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("failed to add secret: %s", resp.Error)
	}

	fmt.Printf("Secret %q stored for skill %q\n", name, secretsSkillID)
	return nil
}

func runSecretsList(cmd *cobra.Command, args []string) error {
	client := api.NewClient(resolveDaemonSocketPath())
	resp, err := client.Call(cmd.Context(), "vault.secret.list", api.VaultSecretListRequest{})
	if err != nil {
		return fmt.Errorf("daemon call failed: %w\n  (Is the daemon running? Start with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("failed to list secrets: %s", resp.Error)
	}

	var entries []api.VaultSecretEntry
	if resp.Data != nil {
		if err := json.Unmarshal(resp.Data, &entries); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	if len(entries) == 0 {
		fmt.Println("No secrets stored.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSKILL\tCREATED\tUPDATED")
	fmt.Fprintln(w, "----\t-----\t-------\t-------")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.SkillID, e.CreatedAt, e.UpdatedAt)
	}
	w.Flush()

	return nil
}

func runSecretsRotate(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Read new secret from secure prompt before contacting the daemon.
	value, err := readSecretFromTerminal("Enter new secret value: ")
	if err != nil {
		return fmt.Errorf("failed to read secret: %w", err)
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	// skill_id is optional for rotate; the daemon will keep the existing
	// association when it is absent.  We pass it through only if provided.
	req := api.VaultSecretAddRequest{
		Name:   name,
		Value:  value,
		Rotate: true,
	}
	if secretsSkillID != "" {
		req.SkillID = secretsSkillID
	}

	client := api.NewClient(resolveDaemonSocketPath())
	resp, err := client.Call(cmd.Context(), "vault.secret.rotate", req)
	if err != nil {
		return fmt.Errorf("daemon call failed: %w\n  (Is the daemon running? Start with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("failed to rotate secret: %s", resp.Error)
	}

	fmt.Printf("Secret %q rotated\n", name)
	return nil
}

func runSecretsRefresh(cmd *cobra.Command, args []string) error {
	if secretsSkillID == "" {
		return fmt.Errorf("--skill flag is required")
	}

	client := api.NewClient(resolveDaemonSocketPath())
	reqData, _ := json.Marshal(map[string]string{"name": secretsSkillID})
	resp, err := client.Call(cmd.Context(), "skill.secrets.refresh", json.RawMessage(reqData))
	if err != nil {
		return fmt.Errorf("daemon call failed: %w\n  (Is the daemon running? Start with: sudo aegisclaw start)", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("secrets refresh failed: %s", resp.Error)
	}

	// Parse response to report injected count.
	var result struct {
		Injected int `json:"injected"`
	}
	if resp.Data != nil {
		_ = json.Unmarshal(resp.Data, &result)
	}
	fmt.Printf("Refreshed %d secret(s) in running skill VM %q\n", result.Injected, secretsSkillID)
	return nil
}
