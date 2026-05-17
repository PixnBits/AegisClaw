package main

// NOTE: This file is in transition as part of the aggressive Court extraction (Task 03).
// Most of its logic (initCourtEngine, makeCourtRoundUpdater, etc.) has been removed
// from the Host Daemon startup path.
//
// During stabilization, this file is largely neutralized. The real Court logic
// will eventually live in dedicated Court components (Court Scribe + persona VMs).
//
// TODO: Either delete this file or move remaining useful pieces (e.g. launcher logic)
// to a more appropriate location once the Court extraction is further along.

import (
	"context"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// initCourtEngine is disabled during aggressive Court extraction.
// The Host Daemon no longer owns Court initialization.
func initCourtEngine(env *runtimeEnv, toolRegistry *ToolRegistry) (*court.Engine, error) {
	return nil, nil // intentionally disabled
}

// makeCourtRoundUpdater is disabled during aggressive Court extraction.
func makeCourtRoundUpdater(env *runtimeEnv, toolRegistry *ToolRegistry) court.RoundUpdateFunc {
	return func(ctx context.Context, p *proposal.Proposal, feedback *court.IterationFeedback) (*proposal.Proposal, error) {
		return nil, nil // intentionally disabled
	}
}

// All other functions in this file (initCourtLauncher, prompt builders, etc.)
// are currently unused and will be cleaned up or relocated as Court extraction progresses.
