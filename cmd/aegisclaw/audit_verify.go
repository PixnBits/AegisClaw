package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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
	verifiedHead, headErr := readMerkleHashAtIndex(auditPath, verified)
	if headErr != nil {
		return fmt.Errorf("verified chain but failed to read matched head: %w", headErr)
	}
	fmt.Printf("  Verified head (prefix target): %s (partial verification)\n", verifiedHead)
	return nil
}

func readMerkleHashAtIndex(path string, index uint64) (string, error) {
	if index == 0 {
		return "", fmt.Errorf("index must be greater than 0")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := uint64(0)
	for scanner.Scan() {
		lineNo++
		if lineNo != index {
			continue
		}
		var row struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return "", fmt.Errorf("decode entry %d: %w", lineNo, err)
		}
		if strings.TrimSpace(row.Hash) == "" {
			return "", fmt.Errorf("entry %d has empty hash", lineNo)
		}
		return row.Hash, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan audit log: %w", err)
	}
	return "", fmt.Errorf("entry %d not found", index)
}
