package main

import (
	"fmt"
	"path/filepath"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/spf13/cobra"
)

func runAuditVerify(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	auditPath := filepath.Join(env.Config.Audit.Dir, "kernel.merkle.jsonl")

	fmt.Printf("Verifying Merkle audit chain: %s\n", auditPath)

	verified, err := audit.VerifyChain(auditPath, env.Kernel.PublicKey())
	if err != nil {
		fmt.Printf("  FAIL: chain verification error at entry %d: %v\n", verified+1, err)
		return fmt.Errorf("audit chain verification failed: %w", err)
	}

	fmt.Printf("  OK: %d entries verified\n", verified)
	fmt.Printf("  Chain head: %s\n", env.Kernel.AuditLog().LastHash())
	return nil
}
