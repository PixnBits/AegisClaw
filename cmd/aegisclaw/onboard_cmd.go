package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "Interactive first-run setup wizard",
	Long: `Guides you through the AegisClaw setup process step by step.

Onboarding performs the following steps:
  1. Check system prerequisites (firecracker, jailer, KVM access)
  2. Create the workspace directory (~/.aegisclaw/workspace/)
  3. Write starter AGENTS.md, SOUL.md, TOOLS.md, and SKILL.md templates
  4. Run aegisclaw init if not already initialized
  5. Print next steps

This command is safe to run multiple times — existing files are not
overwritten unless --force is passed.

Inspired by OpenClaw's onboard UX (openclaw onboard), adapted with
AegisClaw's security-first principles.`,
	RunE: runOnboard,
}

var onboardForce bool

func init() {
	onboardCmd.Flags().BoolVar(&onboardForce, "force", false, "Overwrite existing workspace files")
	rootCmd.AddCommand(onboardCmd)
}

func runOnboard(_ *cobra.Command, _ []string) error {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	fmt.Println("Welcome to AegisClaw onboarding!")
	fmt.Println(strings.Repeat("─", 50))

	// ── Step 1: Prerequisites ─────────────────────────────────────────────────
	fmt.Println("\n[1/5] Checking prerequisites...")

	allPrereqsOK := true

	cfg, err := config.Load(logger)
	if err != nil {
		fmt.Printf("  ⚠  Could not load config: %v\n", err)
		def := config.DefaultConfig()
		cfg = &def
	}

	prereqs := []struct {
		name string
		path string
	}{
		{"firecracker", cfg.Firecracker.Bin},
		{"jailer", cfg.Jailer.Bin},
	}
	for _, p := range prereqs {
		if _, err := os.Stat(p.path); err != nil {
			fmt.Printf("  ✗  %s not found at %s\n", p.name, p.path)
			fmt.Printf("     Install from https://github.com/firecracker-microvm/firecracker/releases\n")
			allPrereqsOK = false
		} else {
			fmt.Printf("  ✓  %s found\n", p.name)
		}
	}

	// Check KVM access (Linux only).
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/dev/kvm"); err != nil {
			fmt.Println("  ✗  /dev/kvm not found — KVM is required for Firecracker")
			fmt.Println("     Enable virtualisation in BIOS / run on a KVM-capable host")
			allPrereqsOK = false
		} else {
			fmt.Println("  ✓  /dev/kvm present")
		}
	}

	if !allPrereqsOK {
		fmt.Println("\n  ⚠  Some prerequisites are missing.  AegisClaw may not function correctly.")
		fmt.Println("     Continue anyway? (Ctrl-C to abort, Enter to continue)")
		fmt.Scanln() //nolint:errcheck
	}

	// ── Step 2: Workspace directory ───────────────────────────────────────────
	fmt.Println("\n[2/5] Setting up workspace directory...")
	wsDir := cfg.Workspace.Dir
	if wsDir == "" {
		home, _ := os.UserHomeDir()
		wsDir = filepath.Join(home, ".aegisclaw", "workspace")
	}
	if err := os.MkdirAll(wsDir, 0700); err != nil {
		fmt.Printf("  ✗  Could not create workspace dir %s: %v\n", wsDir, err)
	} else {
		fmt.Printf("  ✓  Workspace directory: %s\n", wsDir)
	}

	// ── Step 3: Write starter workspace files ─────────────────────────────────
	fmt.Println("\n[3/5] Writing starter workspace files...")

	wsFiles := map[string]string{
		"AGENTS.md": "# Agent Identity\n\n" +
			"You are a helpful, security-conscious assistant powered by AegisClaw.\n" +
			"You help users manage skills that run in isolated sandboxes.\n" +
			"Be warm, helpful, and concise.\n\n" +
			"<!-- Edit this file to customise the agent's persona. -->\n",

		"SOUL.md": "# Guiding Principles\n\n" +
			"1. Security first — never bypass isolation or audit logging.\n" +
			"2. Transparency — explain tool actions before executing them.\n" +
			"3. Respect user privacy — apply PII redaction when in doubt.\n" +
			"4. Minimal footprint — request only the capabilities you need.\n\n" +
			"<!-- Edit this file to set the agent's values and principles. -->\n",

		"TOOLS.md": "# Tool Preferences\n\n" +
			"- Prefer `script.run` for short, bounded computation.\n" +
			"- Use `spawn_worker` for research and multi-step tasks.\n" +
			"- Always call `list_pending_async` before scheduling new timers.\n\n" +
			"<!-- Edit this file to add tool preference hints. -->\n",

		"SKILL.md": "# Skill Build Context\n\n" +
			"This file is injected into the system prompt during skill code generation.\n" +
			"Use it to provide project-specific context to the Builder.\n\n" +
			"<!-- Example: project language, coding style, key libraries, constraints. -->\n",
	}

	for name, content := range wsFiles {
		path := filepath.Join(wsDir, name)
		if _, err := os.Stat(path); err == nil && !onboardForce {
			fmt.Printf("  –  %s already exists (use --force to overwrite)\n", name)
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			fmt.Printf("  ✗  Could not write %s: %v\n", name, err)
		} else {
			fmt.Printf("  ✓  %s\n", name)
		}
	}

	// ── Step 4: Run aegisclaw init ────────────────────────────────────────────
	fmt.Println("\n[4/5] Initializing AegisClaw...")

	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".config", "aegisclaw", "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("  –  Already initialized (config.yaml exists)")
	} else {
		// Find our own executable path and re-invoke init.
		self, err := os.Executable()
		if err != nil {
			self = "aegisclaw"
		}
		cmd := exec.Command(self, "init")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  ✗  aegisclaw init failed: %v\n", err)
		} else {
			fmt.Println("  ✓  Initialized successfully")
		}
	}

	// ── Step 5: Next steps ────────────────────────────────────────────────────
	fmt.Println("\n[5/5] Done!")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println()
	fmt.Println("  1. Start the daemon:")
	fmt.Println("       aegisclaw start")
	fmt.Println()
	fmt.Println("  2. Open a chat session:")
	fmt.Println("       aegisclaw chat")
	fmt.Println()
	fmt.Println("  3. Check system health:")
	fmt.Println("       aegisclaw doctor")
	fmt.Println()
	fmt.Printf("  4. Edit your workspace files in %s\n", wsDir)
	fmt.Println("       AGENTS.md  — agent persona")
	fmt.Println("       SOUL.md    — guiding principles")
	fmt.Println("       TOOLS.md   — tool preference hints")
	fmt.Println("       SKILL.md   — skill build context")
	fmt.Println()
	fmt.Println("  5. Create your first skill:")
	fmt.Println("       aegisclaw chat")
	fmt.Println(`       > "Create a skill that fetches the weather"`)
	fmt.Println()

	return nil
}
