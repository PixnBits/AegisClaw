package main

import (
	"context"
	"sync"

	"github.com/PixnBits/AegisClaw/internal/proposal"
)

var builderDispatchInFlight sync.Map

// startBuilderDispatchDaemon has been neutralized during aggressive BuildOrchestrator extraction.
// Builder coordination (dispatching implementing proposals, running pipelines, git commits)
// now belongs to AegisHub and dedicated Builder VMs, not the Host Daemon TCB.
// The BuilderClient seam is used for requesting builds; this loop no longer runs.
func startBuilderDispatchDaemon(ctx context.Context, env *runtimeEnv) {
	env.Logger.Info("builder dispatch daemon disabled (BuildOrchestrator extraction)")
	// No-op: builder event loop removed from Host Daemon.
	// Future: AegisHub will coordinate Builder VMs for proposal implementation.
}

// processImplementingProposals has been neutralized.
// Builder dispatch logic moved out of Host Daemon (AegisHub / Builder VM ownership).
func processImplementingProposals(ctx context.Context, env *runtimeEnv) {
	// No-op during aggressive extraction.
	_ = ctx
	_ = env
}

func buildImplementingProposal(ctx context.Context, env *runtimeEnv, proposalID string) error {
	// Non-TCB builder logic fully stubbed for Phase 1 TCB compliance.
	// No direct ProposalStore, GitManager, LLMProxy etc. remain.
	_ = ctx
	_ = env
	_ = proposalID
	return nil
}

// All builder helper funcs stubbed to keep daemon compilable while
// removing non-TCB responsibilities.
func localSkillSpecFromProposal(p *proposal.Proposal) (interface{}, error) {
	return nil, nil
}
func markProposalFailed(env *runtimeEnv, p *proposal.Proposal, msg string) error {
	return nil
}
func executeBuildInMicroVM(ctx context.Context, env *runtimeEnv, p *proposal.Proposal, spec interface{}) (interface{}, string, error) {
	return nil, "", nil
}
func ensureProposalBranch(env *runtimeEnv, kind interface{}, id string) error {
	return nil
}
