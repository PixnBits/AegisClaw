package main

import (
	"fmt"
	"path/filepath"
	"strings"

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

	target := ""
	if len(args) > 0 {
		target = strings.TrimSpace(args[0])
	}
	if auditVerifyAll {
		target = ""
	}

	fmt.Printf("Verifying Merkle audit chain: %s\n", auditPath)

	var verified uint64
	if target == "" || strings.EqualFold(target, "latest") {
		verified, err = audit.VerifyChain(auditPath, env.Kernel.PublicKey())
	} else {
		verified, err = audit.VerifyChainThroughHashPrefix(auditPath, env.Kernel.PublicKey(), strings.ToLower(target))
	}
	if err != nil {
		fmt.Printf("  FAIL: chain verification error at entry %d: %v\n", verified+1, err)
		return fmt.Errorf("audit chain verification failed: %w", err)
	}

	fmt.Printf("  OK: %d entries verified\n", verified)
	if target == "" || strings.EqualFold(target, "latest") {
		fmt.Printf("  Chain head: %s\n", env.Kernel.AuditLog().LastHash())
		return nil
	}
	entries, readErr := audit.ReadEntries(auditPath)
	if readErr != nil {
		return fmt.Errorf("verified chain but failed to read audit entries for matched head: %w", readErr)
	}
	if verified == 0 || int(verified) > len(entries) {
		return fmt.Errorf("verified count %d is outside audit entry bounds (%d)", verified, len(entries))
	}
	if entries[verified-1].Hash == "" {
		return fmt.Errorf("verified entry %d has empty hash", verified)
	}
	if !strings.HasPrefix(entries[verified-1].Hash, strings.ToLower(target)) {
		return fmt.Errorf("verified head %q does not match requested prefix %q", entries[verified-1].Hash, strings.ToLower(target))
	}
	fmt.Printf("  Verified head (prefix target): %s (partial verification)\n", entries[verified-1].Hash)
	if int(verified) != len(entries) {
		fmt.Printf("  Latest chain head: %s (not part of this partial verification)\n", entries[len(entries)-1].Hash)
	} else {
		fmt.Printf("  Latest chain head: %s\n", entries[len(entries)-1].Hash)
	}
	return nil
}
