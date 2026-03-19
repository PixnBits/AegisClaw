#!/usr/bin/env python3
"""Writes cmd/aegisclaw/secret_cmd.go — CLI commands for secret vault."""
import os

code = r'''package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/vault"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var secretSkillID string

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage kernel-encrypted secrets",
	Long:  `Commands for adding, listing, and deleting age-encrypted secrets managed by the kernel.`,
}

var secretAddCmd = &cobra.Command{
	Use:   "add <name> <value>",
	Short: "Add or update a secret",
	Long: `Encrypts and stores a secret value using age encryption.
The secret is associated with a skill via the --skill flag.
Secret values are never stored in plaintext on disk.`,
	Args: cobra.ExactArgs(2),
	RunE: runSecretAdd,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored secrets",
	Long:  `Displays metadata for all secrets in the vault. Secret values are never shown.`,
	RunE:  runSecretList,
}

var secretDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a secret",
	Long:  `Permanently removes an encrypted secret from the vault.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretDelete,
}

func runSecretAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	value := args[1]

	if secretSkillID == "" {
		return fmt.Errorf("--skill flag is required")
	}

	env, err := initRuntime()
	if err != nil {
		return err
	}

	v, err := vault.NewVault(env.Config.Vault.Dir, env.Kernel.PrivateKeyBytes(), env.Logger)
	if err != nil {
		return fmt.Errorf("failed to open vault: %w", err)
	}

	if err := v.Add(name, secretSkillID, []byte(value)); err != nil {
		return fmt.Errorf("failed to add secret: %w", err)
	}

	// Audit log the secret addition (value is never logged)
	auditPayload := fmt.Appendf(nil, `{"name":%q,"skill_id":%q}`, name, secretSkillID)
	action := kernel.NewAction(kernel.ActionSecretAdd, "cli", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit log secret add", zap.Error(logErr))
	}

	fmt.Printf("Secret %q stored for skill %q\n", name, secretSkillID)
	return nil
}

func runSecretList(cmd *cobra.Command, args []string) error {
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

func runSecretDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}

	v, err := vault.NewVault(env.Config.Vault.Dir, env.Kernel.PrivateKeyBytes(), env.Logger)
	if err != nil {
		return fmt.Errorf("failed to open vault: %w", err)
	}

	if err := v.Delete(name); err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	// Audit log the deletion
	auditPayload := fmt.Appendf(nil, `{"name":%q}`, name)
	action := kernel.NewAction(kernel.ActionSecretDelete, "cli", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit log secret delete", zap.Error(logErr))
	}

	fmt.Printf("Secret %q deleted\n", name)
	return nil
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'cmd', 'aegisclaw', 'secret_cmd.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"secret_cmd.go: {len(code)} bytes -> {outpath}")
