package main

// Court initialization stubs for the Host Daemon Minimal TCB.
// Real Court logic (review, voting, round updates, and sandbox management)
// now runs in dedicated Court VMs and is orchestrated by the Court Scribe.
// The Host Daemon only forwards requests via CourtClient.

import (
	"context"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// initCourtEngine is a stub. The real Court engine runs in Court VMs.
func initCourtEngine(env *runtimeEnv, toolRegistry *ToolRegistry) (*court.Engine, error) {
	return nil, nil
}

// makeCourtRoundUpdater is a stub. Real round update logic lives in Court VMs.
func makeCourtRoundUpdater(env *runtimeEnv, toolRegistry *ToolRegistry) court.RoundUpdateFunc {
	return func(ctx context.Context, p *proposal.Proposal, feedback *court.IterationFeedback) (*proposal.Proposal, error) {
		return nil, nil
	}
}

// initCourtLauncher is a stub. Court sandbox launching is handled externally.
func initCourtLauncher(env *runtimeEnv) (court.SandboxLauncher, error) {
	return nil, nil
}

// Note: Legacy Court helpers were removed during the Minimal TCB refactor.
// Court responsibilities now live outside the Host Daemon.