package main

// Court initialization stubs for the Host Daemon Minimal TCB.
// Real Court logic (review, voting, round updates) now runs in dedicated Court VMs
// and is orchestrated by the Court Scribe. The Host Daemon only forwards requests
// via CourtClient.

import (
	"context"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// initCourtEngine returns a nil engine; Court logic lives in Court VMs + Court Scribe.
func initCourtEngine(env *runtimeEnv, toolRegistry *ToolRegistry) (*court.Engine, error) {
	return nil, nil
}

// makeCourtRoundUpdater returns a no-op updater; real round logic is in Court VMs.
func makeCourtRoundUpdater(env *runtimeEnv, toolRegistry *ToolRegistry) court.RoundUpdateFunc {
	return func(ctx context.Context, p *proposal.Proposal, feedback *court.IterationFeedback) (*proposal.Proposal, error) {
		return nil, nil
	}
}

// initCourtLauncher returns nil; sandbox launching for Court is handled externally.
func initCourtLauncher(env *runtimeEnv) (court.SandboxLauncher, error) {
	return nil, nil
}

// Note: legacy helpers (buildRoundUpdaterSystemPrompt, etc.) were removed during
// the Minimal TCB refactor; Court responsibilities have moved out of the Host Daemon.
