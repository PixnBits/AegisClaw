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

type BuildOrchestrator struct {
	pipeline   *Pipeline
	store      *proposal.Store
	kern       *kernel.Kernel
	logger     *zap.Logger
	dispatcher *events.ProposalEventDispatcher

	mu     sync.Mutex
	active map[string]bool
}

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

func (o *BuildOrchestrator) Start(ctx context.Context) {
	o.dispatcher.Subscribe(o.handleProposalEvent)
	o.logger.Info("BuildOrchestrator started")
}

func (o *BuildOrchestrator) handleProposalEvent(event events.ProposalStatusChangedEvent) {
	if event.To != proposal.StatusImplementing {
		return
	}

	o.mu.Lock()
	if o.active[event.ProposalID] {
		o.mu.Unlock()
		return
	}
	o.active[event.ProposalID] = true
	o.mu.Unlock()

	go o.runBuild(context.Background(), event.ProposalID)
}

func (o *BuildOrchestrator) runBuild(ctx context.Context, proposalID string) {
	defer func() {
		o.mu.Lock()
		delete(o.active, proposalID)
		o.mu.Unlock()
	}()

	p, err := o.store.Get(proposalID)
	if err != nil {
		o.logger.Error("failed to load proposal", zap.String("id", proposalID), zap.Error(err))
		return
	}

	if !p.IsApproved() && p.Status != proposal.StatusImplementing {
		return
	}

	spec := &SkillSpec{Name: p.TargetSkill, Description: p.Description}

	o.logger.Info("starting builder pipeline", zap.String("proposal_id", proposalID))

	_, err = o.pipeline.Execute(ctx, p, spec)
	if err != nil {
		o.logger.Error("builder pipeline failed", zap.Error(err))
		return
	}

	o.logger.Info("builder pipeline completed", zap.String("proposal_id", proposalID))
}

func (o *BuildOrchestrator) deriveSkillSpec(p *proposal.Proposal) *SkillSpec {
	name := p.TargetSkill
	if name == "" {
		name = "skill-" + p.ID[:8]
	}
	return &SkillSpec{Name: name, Description: p.Description}
}
