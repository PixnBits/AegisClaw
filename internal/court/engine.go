package court

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ReviewerFunc is the function signature for running a single persona review.
// Implementations handle sandbox creation, prompt injection, and response parsing.
type ReviewerFunc func(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error)

// EngineConfig holds configuration for the Court Engine.
type EngineConfig struct {
	MaxRounds        int           `yaml:"max_rounds" mapstructure:"max_rounds"`
	ReviewTimeout    time.Duration `yaml:"review_timeout" mapstructure:"review_timeout"`
	ConsensusQuorum  float64       `yaml:"consensus_quorum" mapstructure:"consensus_quorum"`
	MaxRiskThreshold float64       `yaml:"max_risk_threshold" mapstructure:"max_risk_threshold"`
}

// DefaultEngineConfig returns production defaults.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxRounds:        3,
		ReviewTimeout:    5 * time.Minute,
		ConsensusQuorum:  0.8,
		MaxRiskThreshold: 7.0,
	}
}

// SessionState represents the current state of a court session.
type SessionState string

const (
	SessionPending   SessionState = "pending"
	SessionReviewing SessionState = "reviewing"
	SessionConsensus SessionState = "consensus"
	SessionApproved  SessionState = "approved"
	SessionRejected  SessionState = "rejected"
	SessionEscalated SessionState = "escalated"
)

// Session tracks one full court review of a proposal.
type Session struct {
	ID            string             `json:"id"`
	ProposalID    string             `json:"proposal_id"`
	State         SessionState       `json:"state"`
	Round         int                `json:"round"`
	Personas      []string           `json:"personas"`
	Results       []RoundResult      `json:"results"`
	StartedAt     time.Time          `json:"started_at"`
	EndedAt       *time.Time         `json:"ended_at,omitempty"`
	Verdict       string             `json:"verdict,omitempty"`
	RiskScore     float64            `json:"risk_score"`
	PriorFeedback *IterationFeedback `json:"prior_feedback,omitempty"`
}

// RoundResult captures all reviews for a single round.
type RoundResult struct {
	Round     int                `json:"round"`
	Reviews   []proposal.Review  `json:"reviews"`
	Heatmap   map[string]float64 `json:"heatmap"`
	AvgRisk   float64            `json:"avg_risk"`
	Consensus bool               `json:"consensus"`
	Feedback  *IterationFeedback `json:"feedback,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
}

// Engine orchestrates the court review process.
type Engine struct {
	config     EngineConfig
	store      *proposal.Store
	kernel     *kernel.Kernel
	personas   []*Persona
	reviewerFn ReviewerFunc
	logger     *zap.Logger
	mu         sync.Mutex
	sessions   map[string]*Session
}

// NewEngine creates a Court Engine.
func NewEngine(cfg EngineConfig, store *proposal.Store, kern *kernel.Kernel, personas []*Persona, reviewerFn ReviewerFunc, logger *zap.Logger) (*Engine, error) {
	if store == nil {
		return nil, fmt.Errorf("proposal store is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}
	if len(personas) == 0 {
		return nil, fmt.Errorf("at least one persona is required")
	}
	if reviewerFn == nil {
		return nil, fmt.Errorf("reviewer function is required")
	}
	if cfg.MaxRounds < 1 {
		return nil, fmt.Errorf("max rounds must be >= 1")
	}
	if cfg.ConsensusQuorum <= 0 || cfg.ConsensusQuorum > 1 {
		return nil, fmt.Errorf("consensus quorum must be between 0 and 1")
	}
	if cfg.MaxRiskThreshold <= 0 || cfg.MaxRiskThreshold > 10 {
		return nil, fmt.Errorf("max risk threshold must be between 0 and 10")
	}

	return &Engine{
		config:     cfg,
		store:      store,
		kernel:     kern,
		personas:   personas,
		reviewerFn: reviewerFn,
		logger:     logger,
		sessions:   make(map[string]*Session),
	}, nil
}

// Review starts or continues a court review session for a proposal.
func (e *Engine) Review(ctx context.Context, proposalID string) (*Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, err := e.store.Get(proposalID)
	if err != nil {
		return nil, fmt.Errorf("failed to load proposal: %w", err)
	}

	// Transition proposal to in_review if it is submitted
	if p.Status == proposal.StatusSubmitted {
		if err := p.Transition(proposal.StatusInReview, "court session started", "court-engine"); err != nil {
			return nil, fmt.Errorf("failed to transition proposal to in_review: %w", err)
		}
		if err := e.store.Update(p); err != nil {
			return nil, fmt.Errorf("failed to persist proposal transition: %w", err)
		}
	} else if p.Status != proposal.StatusInReview {
		return nil, fmt.Errorf("proposal must be in submitted or in_review status, got %q", p.Status)
	}

	// Log the review action
	payload, _ := json.Marshal(map[string]string{"proposal_id": proposalID})
	action := kernel.NewAction(kernel.ActionProposalReview, "court-engine", payload)
	if _, err := e.kernel.SignAndLog(action); err != nil {
		return nil, fmt.Errorf("failed to log review action: %w", err)
	}

	session := e.getOrCreateSession(p)

	// Run review rounds with iteration feedback
	for session.Round < e.config.MaxRounds {
		session.Round++
		session.State = SessionReviewing
		p.Round = session.Round

		e.logger.Info("starting review round",
			zap.String("session_id", session.ID),
			zap.String("proposal_id", proposalID),
			zap.Int("round", session.Round),
			zap.Int("persona_count", len(e.personas)),
			zap.Bool("has_prior_feedback", session.PriorFeedback != nil && session.PriorFeedback.HasQuestions),
		)

		result, err := e.runRound(ctx, p, session.Round)
		if err != nil {
			return session, fmt.Errorf("round %d failed: %w", session.Round, err)
		}

		// Use weighted consensus evaluation
		consensus := EvaluateConsensus(result.Reviews, e.personas, e.config.ConsensusQuorum)
		result.Consensus = consensus.Reached
		result.Heatmap = consensus.Heatmap
		result.AvgRisk = consensus.AvgRisk
		consensus.Feedback.RoundNumber = session.Round
		result.Feedback = &consensus.Feedback

		session.Results = append(session.Results, *result)
		session.RiskScore = result.AvgRisk

		// Persist reviews on proposal
		for _, review := range result.Reviews {
			if err := p.AddReview(review); err != nil {
				e.logger.Error("failed to add review to proposal", zap.Error(err))
			}
		}
		if err := e.store.Update(p); err != nil {
			e.logger.Error("failed to persist proposal with reviews", zap.Error(err))
		}

		if result.Consensus {
			if result.AvgRisk <= e.config.MaxRiskThreshold {
				session.State = SessionApproved
				session.Verdict = "approved"
				return e.finalizeSession(session, p, proposal.StatusApproved, "weighted consensus reached: approved")
			}
			session.State = SessionRejected
			session.Verdict = "rejected"
			return e.finalizeSession(session, p, proposal.StatusRejected, fmt.Sprintf("weighted consensus reached but risk too high: %.1f", result.AvgRisk))
		}

		// Store feedback for next round's iteration
		session.PriorFeedback = result.Feedback
		session.State = SessionConsensus
		e.logger.Info("no consensus, iterating with feedback",
			zap.Int("round", session.Round),
			zap.Float64("approval_rate", consensus.ApprovalRate),
			zap.Float64("avg_risk", result.AvgRisk),
			zap.Int("questions", len(consensus.Feedback.Questions)),
		)
	}

	// Max rounds exhausted without consensus
	session.State = SessionEscalated
	session.Verdict = "escalated"
	now := time.Now().UTC()
	session.EndedAt = &now

	e.logger.Warn("court session escalated: max rounds reached without consensus",
		zap.String("session_id", session.ID),
		zap.String("proposal_id", proposalID),
		zap.Int("rounds", session.Round),
	)
	return session, nil
}

func (e *Engine) runRound(ctx context.Context, p *proposal.Proposal, round int) (*RoundResult, error) {
	type reviewResult struct {
		review *proposal.Review
		err    error
	}

	results := make(chan reviewResult, len(e.personas))
	var wg sync.WaitGroup

	for _, persona := range e.personas {
		wg.Add(1)
		go func(per *Persona) {
			defer wg.Done()

			reviewCtx, cancel := context.WithTimeout(ctx, e.config.ReviewTimeout)
			defer cancel()

			review, err := e.reviewerFn(reviewCtx, p, per)
			results <- reviewResult{review: review, err: err}
		}(persona)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var reviews []proposal.Review
	var errors []error
	for res := range results {
		if res.err != nil {
			errors = append(errors, res.err)
			e.logger.Error("reviewer failed", zap.Error(res.err))
			continue
		}
		if res.review != nil {
			res.review.Round = round
			reviews = append(reviews, *res.review)
		}
	}

	if len(reviews) == 0 {
		return nil, fmt.Errorf("all %d reviewers failed: %v", len(e.personas), errors)
	}

	// Consensus evaluation is done in Review() via EvaluateConsensus.
	// Here we just return the raw reviews; heatmap/consensus are populated by caller.
	return &RoundResult{
		Round:     round,
		Reviews:   reviews,
		Timestamp: time.Now().UTC(),
	}, nil
}

func (e *Engine) getOrCreateSession(p *proposal.Proposal) *Session {
	for _, s := range e.sessions {
		if s.ProposalID == p.ID && s.EndedAt == nil {
			return s
		}
	}

	personaNames := make([]string, len(e.personas))
	for i, per := range e.personas {
		personaNames[i] = per.Name
	}

	session := &Session{
		ID:         uuid.New().String(),
		ProposalID: p.ID,
		State:      SessionPending,
		Round:      0,
		Personas:   personaNames,
		StartedAt:  time.Now().UTC(),
	}
	e.sessions[session.ID] = session
	return session
}

func (e *Engine) finalizeSession(session *Session, p *proposal.Proposal, status proposal.Status, reason string) (*Session, error) {
	now := time.Now().UTC()
	session.EndedAt = &now

	if err := p.Transition(status, reason, "court-engine"); err != nil {
		return session, fmt.Errorf("failed to transition proposal to %s: %w", status, err)
	}

	actionType := kernel.ActionProposalApprove
	if status == proposal.StatusRejected {
		actionType = kernel.ActionProposalReject
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID,
		"verdict":     session.Verdict,
		"risk_score":  session.RiskScore,
		"rounds":      session.Round,
	})
	action := kernel.NewAction(actionType, "court-engine", payload)
	if _, err := e.kernel.SignAndLog(action); err != nil {
		e.logger.Error("failed to log court verdict", zap.Error(err))
	}

	if err := e.store.Update(p); err != nil {
		return session, fmt.Errorf("failed to persist final proposal state: %w", err)
	}

	e.logger.Info("court session finalized",
		zap.String("session_id", session.ID),
		zap.String("verdict", session.Verdict),
		zap.Float64("risk_score", session.RiskScore),
		zap.Int("rounds", session.Round),
	)
	return session, nil
}

// GetSession returns a session by ID.
func (e *Engine) GetSession(id string) (*Session, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[id]
	return s, ok
}

// ActiveSessions returns all non-finalized sessions.
func (e *Engine) ActiveSessions() []*Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	var active []*Session
	for _, s := range e.sessions {
		if s.EndedAt == nil {
			active = append(active, s)
		}
	}
	return active
}

// RiskHeatmap returns the heatmap from the latest round of a session.
func (e *Engine) RiskHeatmap(sessionID string) (map[string]float64, error) {
	s, ok := e.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if len(s.Results) == 0 {
		return nil, fmt.Errorf("session has no results yet")
	}
	return s.Results[len(s.Results)-1].Heatmap, nil
}

// VoteOnProposal allows a human operator to cast a decisive vote on a proposal.
// Works on proposals in submitted, in_review, or escalated state. If no court
// session exists in memory (e.g. dashboard launched independently) one is created.
func (e *Engine) VoteOnProposal(ctx context.Context, proposalID string, voter string, approve bool, reason string) (*Session, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if voter == "" {
		return nil, fmt.Errorf("voter identity is required")
	}
	if reason == "" {
		return nil, fmt.Errorf("vote reason is required")
	}

	p, err := e.store.Get(proposalID)
	if err != nil {
		return nil, fmt.Errorf("failed to load proposal: %w", err)
	}

	// Proposal must be in a votable state.
	switch p.Status {
	case proposal.StatusSubmitted, proposal.StatusInReview:
		// ok
	default:
		return nil, fmt.Errorf("proposal must be submitted or in_review to vote, got %q", p.Status)
	}

	// Transition submitted → in_review if needed.
	if p.Status == proposal.StatusSubmitted {
		if err := p.Transition(proposal.StatusInReview, "operator vote", "court-engine"); err != nil {
			return nil, fmt.Errorf("failed to transition to in_review: %w", err)
		}
		if err := e.store.Update(p); err != nil {
			return nil, fmt.Errorf("failed to persist transition: %w", err)
		}
	}

	// Find an existing open session, or create one for the human override.
	var session *Session
	for _, s := range e.sessions {
		if s.ProposalID == proposalID && s.EndedAt == nil {
			session = s
			break
		}
	}
	if session == nil {
		session = &Session{
			ID:         uuid.New().String(),
			ProposalID: proposalID,
			State:      SessionEscalated,
			Round:      p.Round,
			Personas:   []string{},
			StartedAt:  time.Now().UTC(),
		}
		e.sessions[session.ID] = session
	}

	// Record the human vote as a review
	verdict := proposal.VerdictApprove
	targetStatus := proposal.StatusApproved
	sessionVerdict := "approved"
	sessionState := SessionApproved
	if !approve {
		verdict = proposal.VerdictReject
		targetStatus = proposal.StatusRejected
		sessionVerdict = "rejected"
		sessionState = SessionRejected
	}

	humanReview := proposal.Review{
		ID:        uuid.New().String(),
		Persona:   "human:" + voter,
		Model:     "human",
		Round:     session.Round + 1,
		Verdict:   verdict,
		RiskScore: session.RiskScore,
		Evidence:  []string{fmt.Sprintf("Human vote by %s: %s", voter, reason)},
		Comments:  reason,
		Timestamp: time.Now().UTC(),
	}

	if err := p.AddReview(humanReview); err != nil {
		e.logger.Error("failed to add human vote to proposal", zap.Error(err))
	}

	// Log the vote action
	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": proposalID,
		"voter":       voter,
		"approve":     approve,
		"reason":      reason,
	})
	action := kernel.NewAction(kernel.ActionProposalVote, voter, payload)
	if _, err := e.kernel.SignAndLog(action); err != nil {
		e.logger.Error("failed to log human vote", zap.Error(err))
	}

	session.State = sessionState
	session.Verdict = sessionVerdict
	return e.finalizeSession(session, p, targetStatus, fmt.Sprintf("human vote by %s: %s", voter, reason))
}
