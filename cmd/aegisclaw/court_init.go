package main

import (
	"fmt"
	"os"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// initCourtEngine initializes the Governance Court engine for the daemon.
// Court operations are managed entirely by the daemon; they are not directly
// accessible via top-level CLI commands per the PRD alignment plan.
//
// D1 (resolved): The default launcher is now FirecrackerLauncher, which
// runs each reviewer persona in an isolated microVM. The DirectLauncher
// is retained as a fallback for environments without KVM/Firecracker
// (detected via /dev/kvm availability or AEGISCLAW_DIRECT_REVIEW=1).
func initCourtEngine(env *runtimeEnv) (*court.Engine, error) {
	personaDir := env.Config.Court.PersonaDir
	if personaDir == "" {
		var err error
		personaDir, err = court.DefaultPersonaDir()
		if err != nil {
			return nil, fmt.Errorf("failed to determine default persona dir: %w", err)
		}
	}

	personas, err := court.LoadPersonas(personaDir, env.Logger)
	if err != nil {
		// Try to create defaults if dir doesn't exist.
		var createDir string
		createDir, err = court.EnsureDefaultPersonas(env.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create default personas: %w", err)
		}
		personaDir = createDir
		personas, err = court.LoadPersonas(personaDir, env.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load personas after creating defaults: %w", err)
		}
	}

	launcher := initCourtLauncher(env)
	reviewer := court.NewReviewer(launcher, 2, env.Logger)
	reviewerFn := court.NewReviewerFunc(reviewer)

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, env.ProposalStore, env.Kernel, personas, reviewerFn, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create court engine: %w", err)
	}

	return engine, nil
}

// initCourtLauncher selects the appropriate SandboxLauncher for the Court.
//
// Selection logic (D1):
//  1. If AEGISCLAW_DIRECT_REVIEW=1 is set, use DirectLauncher (explicit opt-in
//     for development or environments without KVM).
//  2. If /dev/kvm is available and the Firecracker binary exists, use
//     FirecrackerLauncher (PRD-mandated isolation).
//  3. Otherwise, fall back to DirectLauncher with a warning.
func initCourtLauncher(env *runtimeEnv) court.SandboxLauncher {
	// Explicit override: allow DirectLauncher for development/testing.
	if os.Getenv("AEGISCLAW_DIRECT_REVIEW") == "1" {
		env.Logger.Warn("AEGISCLAW_DIRECT_REVIEW=1: using DirectLauncher (no sandbox isolation)",
			zap.String("reason", "explicit_override"),
		)
		ollamaClient := llm.NewClient(llm.ClientConfig{
			Endpoint: env.Config.Ollama.Endpoint,
		})
		return court.NewDirectLauncher(ollamaClient, env.Logger)
	}

	// Check for KVM and Firecracker availability.
	kvmAvailable := isKVMAvailable()
	fcAvailable := isFirecrackerAvailable(env.Config.Firecracker.Bin)

	if kvmAvailable && fcAvailable {
		env.Logger.Info("court reviewers will use Firecracker sandboxes (D1 compliant)",
			zap.String("firecracker", env.Config.Firecracker.Bin),
		)
		rtCfg := sandbox.RuntimeConfig{
			FirecrackerBin: env.Config.Firecracker.Bin,
			JailerBin:      env.Config.Jailer.Bin,
			KernelImage:    env.Config.Sandbox.KernelImage,
			RootfsTemplate: env.Config.Rootfs.Template,
			ChrootBaseDir:  env.Config.Sandbox.ChrootBase,
			StateDir:       env.Config.Sandbox.StateDir,
		}
		return court.NewFirecrackerLauncher(env.Runtime, env.Kernel, rtCfg, env.Logger)
	}

	// Fall back to DirectLauncher when hardware virtualization is unavailable.
	reason := "unknown"
	if !kvmAvailable {
		reason = "kvm_unavailable"
	} else if !fcAvailable {
		reason = "firecracker_not_found"
	}
	env.Logger.Warn("falling back to DirectLauncher for court reviews (PRD deviation D1)",
		zap.String("reason", reason),
		zap.Bool("kvm", kvmAvailable),
		zap.Bool("firecracker", fcAvailable),
	)
	ollamaClient := llm.NewClient(llm.ClientConfig{
		Endpoint: env.Config.Ollama.Endpoint,
	})
	return court.NewDirectLauncher(ollamaClient, env.Logger)
}

// isKVMAvailable checks whether /dev/kvm is accessible.
func isKVMAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// isFirecrackerAvailable checks whether the Firecracker binary exists.
func isFirecrackerAvailable(binPath string) bool {
	if binPath == "" {
		return false
	}
	_, err := os.Stat(binPath)
	return err == nil
}
