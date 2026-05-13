package builder

import (
	"context"
	"fmt"
	"sync"

	"github.com/PixnBits/AegisClaw/internal/events"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// BuildOrchestrator listens for proposal status changes (via the events dispatcher)
// and automatically triggers the builder pipeline when a proposal reaches "implementing".
// This is the event-driven implementation for D3, ensuring long-term decoupling
// between Court approval and code generation.
type BuildOrchestrator struct {
	pipeline   *Pipeline
	store      *proposal.Store
	kern       *kernel.Kernel
	logger     *zap.Logger
	dispatcher *events.ProposalEventDispatcher

	mu     sync.Mutex
	active map[string]bool // proposalID -> build in progress
}

// NewBuildOrchestrator creates a new orchestrator.
func NewBuildOrchestrator(
	p *Pipeline,
	s *proposal.Store,
	k *kernel.Kernel,
	l *zap.Logger,
	d *events.ProposalEventDispatcher,
) (*BuildOrchestrator, error) {
	if p == nil {
		return nil, fmt.Errorf("pipeline is required")
	}
	if s == nil {
		return nil, fmt.Errorf("proposal store is required")
	}
	if k == nil {
		return nil, fmt.Errorf("kernel is required")
	}
	if l == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if d == nil {
		return nil, fmt.Errorf("dispatcher is required")
	}

	return &BuildOrchestrator{
		pipeline:   p,
		store:      s,
		kern:       k,
		logger:     l,
		dispatcher: d,
		active:     make(map[string]bool),
	}, nil
}

// Start subscribes to proposal events and begins listening.
// Call this once at daemon startup.
func (o *BuildOrchestrator) Start(ctx context.Context) {
	o.dispatcher.Subscribe(o.handleProposalEvent)
	o.logger.Info("BuildOrchestrator started and subscribed to proposal events")
}

// handleProposalEvent is the event handler. It reacts to status changes to "implementing".
func (o *BuildOrchestrator) handleProposalEvent(event events.ProposalStatusChangedEvent) {
	if event.To != proposal.StatusImplementing {
		return
	}

	o.mu.Lock()
	if o.active[event.ProposalID] {
		o.mu.Unlock()
		return // already building
	}
	o.active[event.ProposalID] = true
	o.mu.Unlock()

	go o.runBuild(context.Background(), event.ProposalID)
}

// runBuild performs the actual pipeline execution for a proposal.
func (o *BuildOrchestrator) runBuild(ctx context.Context, proposalID string) {
	defer func() {
		o.mu.Lock()
		delete(o.active, proposalID)
		o.mu.Unlock()
	}()

	p, err := o.store.Get(proposalID)
	if err != nil {
		o.logger.Error("orchestrator: failed to load proposal for build", zap.String("proposal_id", proposalID), zap.Error(err))
		return
	}

	if !p.IsApproved() && p.Status != proposal.StatusImplementing {
		o.logger.Warn("orchestrator: proposal no longer in buildable state", zap.String("proposal_id", proposalID), zap.String("status", string(p.Status)))
		return
	}

	// Derive SkillSpec from proposal (best-effort; in production this would be richer)
	spec := o.deriveSkillSpec(p)

	o.logger.Info("orchestrator: starting automatic builder pipeline", zap.String("proposal_id", proposalID), zap.String("skill", spec.Name))

	result, err := o.pipeline.Execute(ctx, p, spec)
	if err != nil {
		o.logger.Error("orchestrator: builder pipeline failed", zap.String("proposal_id", proposalID), zap.Error(err))
		// Optionally transition proposal to failed here
		return
	}

	o.logger.Info("orchestrator: builder pipeline completed successfully",
		zap.String("proposal_id", proposalID),
		zap.String("commit", result.CommitHash),
		zap.String("branch", result.Branch),
	)

	// TODO: Update proposal with build result metadata, advance status if desired, update composition
}

// deriveSkillSpec creates a SkillSpec from the proposal. This is a starting point;
// in a full implementation it would parse prop.Spec JSON into the full struct.
func (o *BuildOrchestrator) deriveSkillSpec(p *proposal.Proposal) *SkillSpec {
	name := p.TargetSkill
	if name == "" {
		name = "skill-from-" + p.ID[:8]
	}
	return &SkillSpec{
		Name:        name,
		Description: p.Description,
		// Tools, Language, Capabilities etc. would be populated from p.Spec or p fields
	}
}
