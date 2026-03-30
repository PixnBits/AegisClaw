package court

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// RoundUpdateFunc updates a proposal after a non-consensus review round.
// Implementations are expected to persist the updated proposal and return
// the latest persisted copy. The next round will not start until this
// function returns successfully.
type RoundUpdateFunc func(ctx context.Context, p *proposal.Proposal, feedback *IterationFeedback) (*proposal.Proposal, error)

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
	roundUpdater RoundUpdateFunc
	logger     *zap.Logger
	mu         sync.Mutex
	sessions   map[string]*Session
	sessionDir string // directory for persisting session JSON files
	auditDir   string // directory for court review logs
}

// NewEngine creates a Court Engine. If sessionDir is non-empty, sessions are
// persisted to that directory as JSON files for audit and restart recovery.
func NewEngine(cfg EngineConfig, store *proposal.Store, kern *kernel.Kernel, personas []*Persona, reviewerFn ReviewerFunc, logger *zap.Logger, auditDir string, sessionDir ...string) (*Engine, error) {
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

	dir := ""
	if len(sessionDir) > 0 && sessionDir[0] != "" {
		dir = sessionDir[0]
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create session directory %q: %w", dir, err)
		}
	}

	e := &Engine{
		config:     cfg,
		store:      store,
		kernel:     kern,
		personas:   personas,
		reviewerFn: reviewerFn,
		logger:     logger,
		sessions:   make(map[string]*Session),
		sessionDir: dir,
		auditDir:   auditDir,
	}

	// Load any previously persisted sessions.
	if dir != "" {
		if err := e.loadSessions(); err != nil {
			logger.Warn("failed to load persisted sessions", zap.Error(err))
		}
	}

	return e, nil
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

		// If prior feedback exists, include it in the proposal copy passed to reviewers
		reviewTarget := p
		if session.PriorFeedback != nil {
			// Create a shallow copy and append formatted feedback to the description
			tmp := *p
			fb := session.PriorFeedback.FormatFeedbackPrompt()
			if fb != "" {
				if tmp.Description == "" {
					tmp.Description = fb
				} else {
					tmp.Description = tmp.Description + "\n\n" + fb
				}
			}
			reviewTarget = &tmp
		}

		result, err := e.runRound(ctx, reviewTarget, session.Round)
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
			// Log review immediately for progress tracking
			if err := e.logReview(proposalID, review); err != nil {
				e.logger.Error("failed to log review", zap.Error(err))
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

		// Before the next round starts, require an explicit proposal update.
		// If a round updater is configured (for example, the agent operator),
		// use it and block until it returns an updated proposal.
		beforeVersion := p.Version
		if e.roundUpdater != nil {
			updated, updateErr := e.roundUpdater(ctx, p, session.PriorFeedback)
			if updateErr != nil {
				e.logger.Error("round updater failed, escalating proposal",
					zap.String("proposal_id", proposalID),
					zap.Int("round", session.Round),
					zap.Error(updateErr),
				)
				session.State = SessionEscalated
				session.Verdict = "escalated"
				reason := fmt.Sprintf("round %d: agent update failed: %v", session.Round, updateErr)
				return e.finalizeSession(session, p, proposal.StatusEscalated, reason)
			}
			if updated == nil || updated.Version <= beforeVersion {
				e.logger.Error("round updater did not advance proposal version, escalating",
					zap.String("proposal_id", proposalID),
					zap.Int("round", session.Round),
					zap.Int("version_before", beforeVersion),
				)
				session.State = SessionEscalated
				session.Verdict = "escalated"
				reason := fmt.Sprintf("round %d: proposal version not advanced after update", session.Round)
				return e.finalizeSession(session, p, proposal.StatusEscalated, reason)
			}
			p = updated
		} else {
			// Fallback behavior when no external updater is configured: persist
			// feedback text directly so proposal history still advances.
			fbText := ""
			if session.PriorFeedback != nil {
				fbText = session.PriorFeedback.FormatFeedbackPrompt()
			}
			if fbText == "" {
				fbText = fmt.Sprintf("Round %d completed without consensus; proposal requires updates before re-review.", session.Round)
			}
			if err := p.ApplyFeedback(fbText, "court-engine", fmt.Sprintf("feedback for round %d", session.Round)); err != nil {
				e.logger.Error("failed to apply feedback to proposal", zap.Error(err))
			} else {
				if err := e.store.Update(p); err != nil {
					e.logger.Error("failed to persist proposal feedback update", zap.Error(err))
				} else {
					e.logger.Info("applied feedback to proposal and persisted update",
						zap.String("proposal_id", p.ID),
						zap.Int("round", session.Round),
					)
				}
			}
		}
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
	return e.finalizeSession(session, p, proposal.StatusEscalated,
		fmt.Sprintf("max rounds (%d) reached without consensus", e.config.MaxRounds))
}

// SetRoundUpdater configures a synchronous proposal updater invoked between
// non-consensus rounds. Passing nil disables the callback.
func (e *Engine) SetRoundUpdater(updater RoundUpdateFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.roundUpdater = updater
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

// logReview appends a single review for a proposal to a log file.
func (e *Engine) logReview(proposalID string, review proposal.Review) error {
	logDir := filepath.Join(e.auditDir, "court-reviews")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	logFile := filepath.Join(logDir, fmt.Sprintf("%s-reviews.log", proposalID))
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("[%s] Round %d - %s (%s, Risk: %.1f)\n",
		review.Timestamp.Format(time.RFC3339),
		review.Round,
		review.Persona,
		review.Verdict,
		review.RiskScore)
	if review.Comments != "" {
		entry += fmt.Sprintf("Comments: %s\n", review.Comments)
	}
	if len(review.Questions) > 0 {
		entry += fmt.Sprintf("Questions: %s\n", strings.Join(review.Questions, "; "))
	}
	if len(review.Evidence) > 0 {
		entry += fmt.Sprintf("Evidence: %s\n", strings.Join(review.Evidence, "; "))
	}
	entry += "\n"
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("failed to write to log file: %w", err)
	}
	return nil
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

	var actionType kernel.ActionType
	switch status {
	case proposal.StatusRejected:
		actionType = kernel.ActionProposalReject
	case proposal.StatusEscalated:
		actionType = kernel.ActionProposalEscalate
	default:
		actionType = kernel.ActionProposalApprove
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

	e.saveSession(session)

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
	case proposal.StatusSubmitted, proposal.StatusInReview, proposal.StatusEscalated:
		// ok
	default:
		return nil, fmt.Errorf("proposal must be submitted, in_review, or escalated to vote, got %q", p.Status)
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

// saveSession persists a session to disk as a JSON file. Called after every
// state change so the review audit trail survives daemon restarts.
func (e *Engine) saveSession(s *Session) {
	if e.sessionDir == "" || s == nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		e.logger.Error("failed to marshal session for persistence", zap.String("session_id", s.ID), zap.Error(err))
		return
	}
	path := filepath.Join(e.sessionDir, s.ID+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		e.logger.Error("failed to persist session", zap.String("path", path), zap.Error(err))
	}
}

// loadSessions reads all persisted session JSON files from the session directory
// and populates the in-memory sessions map. Called once during NewEngine.
func (e *Engine) loadSessions() error {
	entries, err := os.ReadDir(e.sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read session dir: %w", err)
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(e.sessionDir, entry.Name()))
		if err != nil {
			e.logger.Warn("failed to read session file", zap.String("file", entry.Name()), zap.Error(err))
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			e.logger.Warn("failed to parse session file", zap.String("file", entry.Name()), zap.Error(err))
			continue
		}
		e.sessions[s.ID] = &s
		loaded++
	}
	if loaded > 0 {
		e.logger.Info("loaded persisted court sessions", zap.Int("count", loaded))
	}
	return nil
}

// ResumeStalled finds proposals stuck in submitted or in_review status that
// have no active (un-ended) court session and re-queues them for review.
// Call this after engine creation on daemon startup. Reviews run with limited
// concurrency to avoid overwhelming system resources.
func (e *Engine) ResumeStalled(ctx context.Context) int {
	summaries, err := e.store.List()
	if err != nil {
		e.logger.Error("ResumeStalled: failed to list proposals", zap.Error(err))
		return 0
	}

	// Collect proposals that need review. Hold the lock while reading
	// e.sessions to avoid a data race with concurrent Review() goroutines.
	e.mu.Lock()
	var toResume []string
	for _, s := range summaries {
		switch s.Status {
		case proposal.StatusSubmitted, proposal.StatusInReview:
			// Check if there's already an active session for this proposal.
			hasActive := false
			for _, sess := range e.sessions {
				if sess.ProposalID == s.ID && sess.EndedAt == nil {
					hasActive = true
					break
				}
			}
			if hasActive {
				continue
			}

			e.logger.Info("resuming stalled proposal review",
				zap.String("proposal_id", s.ID),
				zap.String("title", s.Title),
				zap.String("status", string(s.Status)),
			)
			toResume = append(toResume, s.ID)
		}
	}
	e.mu.Unlock()

	if len(toResume) == 0 {
		return 0
	}

	e.logger.Info("ResumeStalled: re-queued stalled proposals", zap.Int("count", len(toResume)))

	// Limit concurrency: run at most 2 reviews in parallel to avoid
	// overwhelming the host with Firecracker VMs and LLM calls.
	const maxConcurrent = 2
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup
	for _, proposalID := range toResume {
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire slot
			defer func() { <-sem }() // release slot

			session, err := e.Review(ctx, pid)
			if err != nil {
				e.logger.Error("ResumeStalled: review failed",
					zap.String("proposal_id", pid),
					zap.Error(err),
				)
				return
			}
			e.logger.Info("ResumeStalled: review completed",
				zap.String("proposal_id", pid),
				zap.String("session_id", session.ID),
				zap.String("verdict", session.Verdict),
			)
		}(proposalID)
	}

	// Don't block startup — let reviews finish in background.
	go func() {
		wg.Wait()
		e.logger.Info("ResumeStalled: all stalled reviews finished", zap.Int("count", len(toResume)))
	}()

	return len(toResume)
}
