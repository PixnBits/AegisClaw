package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
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

	sandboxes, _ := env.Runtime.List(context.Background())
	running := 0
	for _, sb := range sandboxes {
		if sb.State == "running" {
			running++
		}
	}

	skills := env.Registry.List()
	active := 0
	for _, sk := range skills {
		if sk.State == "active" {
			active++
		}
	}

	auditLog := env.Kernel.AuditLog()
	rootHash := env.Registry.RootHash()

	if globalJSON {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"version":          version,
			"commit":           buildCommit,
			"build_date":       buildDate,
			"public_key":       hex.EncodeToString(env.Kernel.PublicKey()),
			"sandboxes_total":  len(sandboxes),
			"sandboxes_active": running,
			"skills_total":     len(skills),
			"skills_active":    active,
			"audit_entries":    auditLog.EntryCount(),
			"audit_head":       auditLog.LastHash(),
			"registry_root":    rootHash,
			"health":           "ok",
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("AegisClaw Status:\n")
	fmt.Printf("  Version:     %s (commit: %s)\n", version, buildCommit)
	fmt.Printf("  Public Key:  %x\n", env.Kernel.PublicKey())
	fmt.Printf("  Health:      OK\n")
	fmt.Printf("  Sandboxes:   %d total, %d running\n", len(sandboxes), running)
	fmt.Printf("  Skills:      %d registered, %d active\n", len(skills), active)

	if rootHash != "" {
		display := rootHash
		if len(display) > 16 {
			display = display[:16]
		}
		fmt.Printf("  Registry:    %s\n", display)
	}

	fmt.Printf("  Audit:       %d entries\n", auditLog.EntryCount())
	if lastHash := auditLog.LastHash(); lastHash != "" {
		display := lastHash
		if len(display) > 16 {
			display = display[:16]
		}
		fmt.Printf("  Chain Head:  %s\n", display)
	}

	return nil
}

func runStatusTUI(env *runtimeEnv) error {
	model := tui.NewStatusDashboard()

	model.LoadStatus = func() (tui.StatusInfo, []tui.SandboxRow, []tui.SkillRow, error) {
		info := tui.StatusInfo{
			PublicKeyHex:   hex.EncodeToString(env.Kernel.PublicKey()),
			AuditEntries:   env.Kernel.AuditLog().EntryCount(),
			AuditChainHead: env.Kernel.AuditLog().LastHash(),
			RegistryRoot:   env.Registry.RootHash(),
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
