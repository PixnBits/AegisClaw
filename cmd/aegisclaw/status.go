package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var statusTUI bool

func runStatus(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if statusTUI {
		return runStatusTUI(env)
	}

	fmt.Printf("AegisClaw Kernel Status:\n")
	fmt.Printf("  Public Key: %x\n", env.Kernel.PublicKey())
	fmt.Printf("  Firecracker Binary: %s\n", env.Config.Firecracker.Bin)
	fmt.Printf("  Jailer Binary: %s\n", env.Config.Jailer.Bin)
	fmt.Printf("  Rootfs Template: %s\n", env.Config.Rootfs.Template)
	fmt.Printf("  Kernel Image: %s\n", env.Config.Sandbox.KernelImage)
	fmt.Printf("  Audit Directory: %s\n", env.Config.Audit.Dir)
	fmt.Printf("  Sandbox State: %s\n", env.Config.Sandbox.StateDir)
	fmt.Printf("  Control Plane Listeners: %d\n", env.Kernel.ControlPlane().ActiveListeners())

	sandboxes, err := env.Runtime.List(context.Background())
	if err == nil {
		running := 0
		for _, sb := range sandboxes {
			if sb.State == "running" {
				running++
			}
		}
		fmt.Printf("  Sandboxes: %d total, %d running\n", len(sandboxes), running)
	}

	skills := env.Registry.List()
	active := 0
	for _, sk := range skills {
		if sk.State == "active" {
			active++
		}
	}
	fmt.Printf("  Skills: %d registered, %d active\n", len(skills), active)
	rootHash := env.Registry.RootHash()
	if rootHash != "" {
		fmt.Printf("  Registry Root: %s\n", rootHash[:16])
	}

	// Merkle audit chain info
	auditLog := env.Kernel.AuditLog()
	fmt.Printf("  Audit Entries: %d\n", auditLog.EntryCount())
	if lastHash := auditLog.LastHash(); lastHash != "" {
		fmt.Printf("  Audit Chain Head: %s\n", lastHash[:16])
	}

	return nil
}

func runStatusTUI(env *runtimeEnv) error {
	model := tui.NewStatusDashboard()

	model.LoadStatus = func() (tui.StatusInfo, []tui.SandboxRow, []tui.SkillRow, error) {
		info := tui.StatusInfo{
			PublicKeyHex:   hex.EncodeToString(env.Kernel.PublicKey()),
			AuditEntries:  env.Kernel.AuditLog().EntryCount(),
			AuditChainHead: env.Kernel.AuditLog().LastHash(),
			RegistryRoot:  env.Registry.RootHash(),
		}

		sandboxes, err := env.Runtime.List(context.Background())
		if err != nil {
			return info, nil, nil, fmt.Errorf("failed to list sandboxes: %w", err)
		}

		sbRows := make([]tui.SandboxRow, len(sandboxes))
		for i, sb := range sandboxes {
			sbRows[i] = tui.SandboxRow{
				ID:        sb.Spec.ID,
				Name:      sb.Spec.Name,
				State:     string(sb.State),
				VCPUs:     sb.Spec.Resources.VCPUs,
				MemoryMB:  sb.Spec.Resources.MemoryMB,
				PID:       sb.PID,
				StartedAt: sb.StartedAt,
				GuestIP:   sb.GuestIP,
			}
		}

		skills := env.Registry.List()
		skRows := make([]tui.SkillRow, len(skills))
		for i, sk := range skills {
			skRows[i] = tui.SkillRow{
				Name:        sk.Name,
				SandboxID:   sk.SandboxID,
				State:       string(sk.State),
				ActivatedAt: sk.ActivatedAt,
				Version:     sk.Version,
			}
		}

		return info, sbRows, skRows, nil
	}

	model.StartSandbox = func(id string) error {
		return env.Runtime.Start(context.Background(), id)
	}

	model.StopSandbox = func(id string) error {
		return env.Runtime.Stop(context.Background(), id)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
