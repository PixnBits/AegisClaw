package main

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/llm"
)

// initCourtEngine initializes the Governance Court engine for the daemon.
// Court operations are managed entirely by the daemon; they are not directly
// accessible via top-level CLI commands per the PRD alignment plan.
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

	ollamaClient := llm.NewClient(llm.ClientConfig{
		Endpoint: env.Config.Ollama.Endpoint,
	})

	// TODO(D1): Switch from DirectLauncher to FirecrackerLauncher once
	// reviewer sandboxes are implemented. The DirectLauncher calls Ollama
	// directly on the host which violates the PRD's isolation model.
	launcher := court.NewDirectLauncher(ollamaClient, env.Logger)
	reviewer := court.NewReviewer(launcher, 2, env.Logger)
	reviewerFn := court.NewReviewerFunc(reviewer)

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, env.ProposalStore, env.Kernel, personas, reviewerFn, env.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create court engine: %w", err)
	}

	return engine, nil
}
