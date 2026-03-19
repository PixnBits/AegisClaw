package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage AegisClaw skills",
	Long:  "Commands for activating, deactivating, and listing skills.",
}

var skillActivateCmd = &cobra.Command{
	Use:   "activate <name>",
	Short: "Activate a skill in its own microVM",
	Long:  "Spins up a new Firecracker microVM, registers the skill in the persistent registry with a Merkle hash chain.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillActivate,
}

var skillDeactivateCmd = &cobra.Command{
	Use:   "deactivate <name>",
	Short: "Deactivate a running skill",
	Long:  "Stops the skill's microVM and marks it as inactive in the registry.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillDeactivate,
}

var skillListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List registered skills",
	RunE:  runSkillList,
}

func runSkillActivate(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	ctx := context.Background()

	if existing, ok := env.Registry.Get(name); ok {
		if existing.State == sandbox.SkillStateActive {
			return fmt.Errorf("skill %q is already active (sandbox=%s)", name, existing.SandboxID)
		}
	}

	sandboxID := uuid.New().String()
	spec := sandbox.SandboxSpec{
		ID:   sandboxID,
		Name: fmt.Sprintf("skill-%s", name),
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 256,
		},
		NetworkPolicy: sandbox.NetworkPolicy{
			DefaultDeny: true,
		},
		RootfsPath: env.Config.Rootfs.Template,
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return fmt.Errorf("failed to create sandbox for skill %q: %w", name, err)
	}

	if err := env.Runtime.Start(ctx, sandboxID); err != nil {
		env.Runtime.Delete(ctx, sandboxID)
		return fmt.Errorf("failed to start sandbox for skill %q: %w", name, err)
	}

	info, err := env.Runtime.Status(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("failed to get sandbox status: %w", err)
	}

	entry, err := env.Registry.Register(name, sandboxID, map[string]string{
		"sandbox_name": spec.Name,
		"guest_ip":     info.GuestIP,
	})
	if err != nil {
		env.Runtime.Stop(ctx, sandboxID)
		env.Runtime.Delete(ctx, sandboxID)
		return fmt.Errorf("failed to register skill: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"skill_name": name,
		"sandbox_id": sandboxID,
		"version":    entry.Version,
		"hash":       entry.MerkleHash,
	})
	action := kernel.NewAction(kernel.ActionSkillActivate, "kernel", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log skill activation", zap.Error(signErr))
	}

	env.Logger.Info("skill activated",
		zap.String("name", name),
		zap.String("sandbox_id", sandboxID),
		zap.Int("pid", info.PID),
		zap.String("merkle_hash", entry.MerkleHash),
	)

	fmt.Printf("Skill '%s' activated.\n", name)
	fmt.Printf("  Sandbox: %s (pid=%d)\n", sandboxID, info.PID)
	fmt.Printf("  Registry: v%d hash=%s\n", entry.Version, entry.MerkleHash[:16])
	fmt.Printf("  Root hash: %s\n", env.Registry.RootHash()[:16])
	return nil
}

func runSkillDeactivate(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	ctx := context.Background()

	entry, ok := env.Registry.Get(name)
	if !ok {
		return fmt.Errorf("skill %q not found in registry", name)
	}
	if entry.State != sandbox.SkillStateActive {
		return fmt.Errorf("skill %q is not active (state: %s)", name, entry.State)
	}

	if err := env.Runtime.Stop(ctx, entry.SandboxID); err != nil {
		env.Logger.Warn("failed to stop sandbox, will deactivate anyway",
			zap.String("sandbox_id", entry.SandboxID),
			zap.Error(err),
		)
	}

	if err := env.Registry.Deactivate(name); err != nil {
		return fmt.Errorf("failed to deactivate skill: %w", err)
	}

	fmt.Printf("Skill '%s' deactivated.\n", name)
	return nil
}

func runSkillList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	skills := env.Registry.List()
	if len(skills) == 0 {
		fmt.Println("No skills registered.")
		return nil
	}

	fmt.Printf("%-20s %-36s %-10s %-4s %-16s\n",
		"NAME", "SANDBOX", "STATE", "VER", "HASH")
	for _, sk := range skills {
		hashDisplay := sk.MerkleHash
		if len(hashDisplay) > 16 {
			hashDisplay = hashDisplay[:16]
		}
		fmt.Printf("%-20s %-36s %-10s %-4d %-16s\n",
			sk.Name, sk.SandboxID, sk.State, sk.Version, hashDisplay)
	}

	rootHash := env.Registry.RootHash()
	if len(rootHash) > 16 {
		rootHash = rootHash[:16]
	}
	fmt.Printf("\nRegistry: seq=%d root=%s\n", env.Registry.Sequence(), rootHash)
	return nil
}
