package main

// This file has been heavily neutralized as part of the aggressive Court extraction.
// Most Court logic now lives (or will live) in dedicated Court components.
// The Host Daemon should no longer execute governance or round-updater logic.

import (
	"context"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// initCourtEngine is disabled.
func initCourtEngine(env *runtimeEnv, toolRegistry *ToolRegistry) (*court.Engine, error) {
	return nil, nil
}

// makeCourtRoundUpdater is disabled.
func makeCourtRoundUpdater(env *runtimeEnv, toolRegistry *ToolRegistry) court.RoundUpdateFunc {
	return func(ctx context.Context, p *proposal.Proposal, feedback *court.IterationFeedback) (*proposal.Proposal, error) {
		return nil, nil
	}
}

// initCourtLauncher is disabled.
func initCourtLauncher(env *runtimeEnv) (court.SandboxLauncher, error) {
	return nil, nil
}

// The following helper functions are also disabled during extraction:
// - buildRoundUpdaterSystemPrompt
// - buildRoundUpdaterUserPrompt
// - buildRoundUpdaterNudge
// - truncate
// - extractToolCallFromContent
//
// They can be removed or relocated once the Court extraction is complete.
