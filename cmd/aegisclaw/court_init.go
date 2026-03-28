package main

import (
	"fmt"
	"os"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// initCourtEngine initializes the Governance Court engine for the daemon.
// Court operations are managed entirely by the daemon; they are not directly
// accessible via top-level CLI commands per the PRD alignment plan.
//
// D1 (resolved): The only supported launcher is FirecrackerLauncher, which
// runs each reviewer persona in an isolated microVM. The daemon will fail to
// start if KVM or Firecracker is unavailable. DirectLauncher is no longer
// used in production builds.
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

	launcher, err := initCourtLauncher(env)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize court launcher: %w", err)
	}
	reviewer := court.NewReviewer(launcher, 2, env.Logger)
	reviewerFn := court.NewReviewerFunc(reviewer)

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, env.ProposalStore, env.Kernel, personas, reviewerFn, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create court engine: %w", err)
	}

	return engine, nil
}

// initCourtLauncher returns the FirecrackerLauncher for Court reviewer sandboxes.
//
// D2-c (resolved): DirectLauncher is no longer reachable from this function.
// If KVM or the Firecracker binary is unavailable, initCourtLauncher returns
// an error so the daemon fails fast with a clear message rather than silently
// degrading to unaudited in-process execution.
func initCourtLauncher(env *runtimeEnv) (court.SandboxLauncher, error) {
	kvmAvailable := isKVMAvailable()
	fcAvailable := isFirecrackerAvailable(env.Config.Firecracker.Bin)

	if !kvmAvailable {
		return nil, fmt.Errorf("KVM is not available (/dev/kvm inaccessible); Firecracker-based court review requires KVM")
	}
	if !fcAvailable {
		return nil, fmt.Errorf("Firecracker binary not found at %q; install Firecracker to run court reviews", env.Config.Firecracker.Bin)
	}

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
	return court.NewFirecrackerLauncher(env.Runtime, env.Kernel, rtCfg, env.Logger), nil
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
