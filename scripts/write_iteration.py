#!/usr/bin/env python3
"""Writes internal/builder/iteration.go — Iterative fix loop with Court feedback."""
import os

code = r'''package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

const (
	// MaxFixRounds is the maximum number of automatic fix iterations.
	MaxFixRounds = 3
)

// FixRoundState represents the state of a single fix round.
type FixRoundState string

const (
	FixRoundPending  FixRoundState = "pending"
	FixRoundRunning  FixRoundState = "running"
	FixRoundComplete FixRoundState = "complete"
	FixRoundFailed   FixRoundState = "failed"
)

// ReviewFeedback captures structured feedback from Court reviewers for code fixes.
type ReviewFeedback struct {
	ReviewerPersona string   `json:"reviewer_persona"`
	Verdict         string   `json:"verdict"`
	Comments        string   `json:"comments"`
	Questions       []string `json:"questions,omitempty"`
	Concerns        []string `json:"concerns,omitempty"`
}

// FixRequest contains all the information needed to attempt a code fix.
type FixRequest struct {
	ProposalID    string           `json:"proposal_id"`
	SkillSpec     SkillSpec        `json:"skill_spec"`
	CurrentFiles  map[string]string `json:"current_files"`
	Feedback      []ReviewFeedback `json:"feedback"`
	AnalysisResult *AnalysisResult `json:"analysis_result,omitempty"`
	Round         int              `json:"round"`
}

// Validate checks the fix request.
func (fr *FixRequest) Validate() error {
	if fr.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if err := fr.SkillSpec.Validate(); err != nil {
		return fmt.Errorf("invalid skill spec: %w", err)
	}
	if len(fr.CurrentFiles) == 0 {
		return fmt.Errorf("current files are required")
	}
	if len(fr.Feedback) == 0 && fr.AnalysisResult == nil {
		return fmt.Errorf("feedback or analysis result is required for fix")
	}
	if fr.Round < 2 || fr.Round > MaxFixRounds+1 {
		return fmt.Errorf("fix round must be between 2 and %d, got %d", MaxFixRounds+1, fr.Round)
	}
	return nil
}

// FeedbackSummary returns a formatted string of all feedback for use in prompts.
func (fr *FixRequest) FeedbackSummary() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("=== Fix Round %d Feedback ===\n\n", fr.Round))

	for _, fb := range fr.Feedback {
		b.WriteString(fmt.Sprintf("Reviewer: %s (verdict: %s)\n", fb.ReviewerPersona, fb.Verdict))
		if fb.Comments != "" {
			b.WriteString(fmt.Sprintf("Comments: %s\n", fb.Comments))
		}
		for _, q := range fb.Questions {
			b.WriteString(fmt.Sprintf("  Question: %s\n", q))
		}
		for _, c := range fb.Concerns {
			b.WriteString(fmt.Sprintf("  Concern: %s\n", c))
		}
		b.WriteString("\n")
	}

	if fr.AnalysisResult != nil && !fr.AnalysisResult.Passed {
		b.WriteString("=== Analysis Failures ===\n")
		if !fr.AnalysisResult.TestPassed {
			b.WriteString("Tests FAILED:\n")
			b.WriteString(fr.AnalysisResult.TestOutput)
			b.WriteString("\n")
		}
		if !fr.AnalysisResult.LintPassed {
			b.WriteString("Lint FAILED:\n")
			b.WriteString(fr.AnalysisResult.LintOutput)
			b.WriteString("\n")
		}
		if !fr.AnalysisResult.SecurityPassed {
			b.WriteString("Security scan FAILED:\n")
			b.WriteString(fr.AnalysisResult.SecurityOutput)
			b.WriteString("\n")
		}
		if !fr.AnalysisResult.BuildPassed {
			b.WriteString("Build FAILED:\n")
			b.WriteString(fr.AnalysisResult.BuildOutput)
			b.WriteString("\n")
		}
		for _, f := range fr.AnalysisResult.Findings {
			b.WriteString(fmt.Sprintf("  [%s] %s: %s", f.Severity, f.Tool, f.Message))
			if f.File != "" {
				b.WriteString(fmt.Sprintf(" (%s:%d)", f.File, f.Line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\nPlease fix all issues listed above. Return the complete corrected files.\n")
	return b.String()
}

// FixRound records a single fix iteration attempt.
type FixRound struct {
	Round          int            `json:"round"`
	State          FixRoundState  `json:"state"`
	Files          map[string]string `json:"files,omitempty"`
	Diff           string         `json:"diff,omitempty"`
	CommitHash     string         `json:"commit_hash,omitempty"`
	Analysis       *AnalysisResult `json:"analysis,omitempty"`
	FeedbackUsed   []ReviewFeedback `json:"feedback_used"`
	StartedAt      time.Time      `json:"started_at"`
	CompletedAt    time.Time      `json:"completed_at,omitempty"`
	Duration       time.Duration  `json:"duration,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// IterationResult captures the outcome of the entire fix loop.
type IterationResult struct {
	ProposalID    string       `json:"proposal_id"`
	Rounds        []FixRound   `json:"rounds"`
	FinalRound    int          `json:"final_round"`
	FinalPassed   bool         `json:"final_passed"`
	FinalCommit   string       `json:"final_commit,omitempty"`
	FinalDiff     string       `json:"final_diff,omitempty"`
	TotalDuration time.Duration `json:"total_duration"`
}

// IterationEngine manages the feedback-driven fix loop.
type IterationEngine struct {
	pipeline   *Pipeline
	builderRT  *BuilderRuntime
	codeGen    *CodeGenerator
	analyzer   *Analyzer
	gitMgr     *gitmanager.Manager
	kern       *kernel.Kernel
	logger     *zap.Logger
	mu         sync.Mutex
	iterations map[string]*IterationResult
}

// NewIterationEngine creates an IterationEngine.
func NewIterationEngine(
	pipeline *Pipeline,
	br *BuilderRuntime,
	cg *CodeGenerator,
	az *Analyzer,
	gm *gitmanager.Manager,
	kern *kernel.Kernel,
	logger *zap.Logger,
) (*IterationEngine, error) {
	if pipeline == nil {
		return nil, fmt.Errorf("pipeline is required")
	}
	if br == nil {
		return nil, fmt.Errorf("builder runtime is required")
	}
	if cg == nil {
		return nil, fmt.Errorf("code generator is required")
	}
	if gm == nil {
		return nil, fmt.Errorf("git manager is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}

	return &IterationEngine{
		pipeline:   pipeline,
		builderRT:  br,
		codeGen:    cg,
		analyzer:   az,
		gitMgr:     gm,
		kern:       kern,
		logger:     logger,
		iterations: make(map[string]*IterationResult),
	}, nil
}

// RunFixLoop attempts to fix code based on reviewer feedback, up to MaxFixRounds.
// It re-generates code, re-runs analysis, re-commits, and re-generates diff each round.
func (ie *IterationEngine) RunFixLoop(
	ctx context.Context,
	builderID string,
	prop *proposal.Proposal,
	spec *SkillSpec,
	initialFiles map[string]string,
	feedback []ReviewFeedback,
	analysisResult *AnalysisResult,
) (*IterationResult, error) {
	if prop == nil {
		return nil, fmt.Errorf("proposal is required")
	}
	if spec == nil {
		return nil, fmt.Errorf("skill spec is required")
	}
	if len(initialFiles) == 0 {
		return nil, fmt.Errorf("initial files are required")
	}
	if len(feedback) == 0 && analysisResult == nil {
		return nil, fmt.Errorf("feedback or analysis result is required")
	}

	iterResult := &IterationResult{
		ProposalID: prop.ID,
	}

	ie.mu.Lock()
	ie.iterations[prop.ID] = iterResult
	ie.mu.Unlock()

	currentFiles := initialFiles
	currentFeedback := feedback
	currentAnalysis := analysisResult
	startTime := time.Now()

	repoKind := gitmanager.RepoSkills
	if prop.Category == proposal.CategoryKernelPatch {
		repoKind = gitmanager.RepoSelf
	}

	for round := 2; round <= MaxFixRounds+1; round++ {
		select {
		case <-ctx.Done():
			return iterResult, ctx.Err()
		default:
		}

		fixReq := &FixRequest{
			ProposalID:     prop.ID,
			SkillSpec:      *spec,
			CurrentFiles:   currentFiles,
			Feedback:       currentFeedback,
			AnalysisResult: currentAnalysis,
			Round:          round,
		}

		if err := fixReq.Validate(); err != nil {
			return iterResult, fmt.Errorf("invalid fix request for round %d: %w", round, err)
		}

		ie.logger.Info("starting fix round",
			zap.String("proposal_id", prop.ID),
			zap.Int("round", round),
			zap.Int("feedback_items", len(currentFeedback)),
		)

		fixRound, err := ie.runSingleFixRound(ctx, builderID, prop, spec, fixReq, repoKind)
		if err != nil {
			fixRound.State = FixRoundFailed
			fixRound.Error = err.Error()
			iterResult.Rounds = append(iterResult.Rounds, *fixRound)

			// Audit log fix failure
			ie.logFixRoundAudit(prop.ID, round, false, err.Error())

			return iterResult, fmt.Errorf("fix round %d failed: %w", round, err)
		}

		iterResult.Rounds = append(iterResult.Rounds, *fixRound)

		// Audit log fix round
		ie.logFixRoundAudit(prop.ID, round, fixRound.Analysis == nil || fixRound.Analysis.Passed, "")

		// Check if analysis passed
		if fixRound.Analysis != nil && fixRound.Analysis.Passed {
			iterResult.FinalRound = round
			iterResult.FinalPassed = true
			iterResult.FinalCommit = fixRound.CommitHash
			iterResult.FinalDiff = fixRound.Diff
			iterResult.TotalDuration = time.Since(startTime)

			ie.logger.Info("fix loop succeeded",
				zap.String("proposal_id", prop.ID),
				zap.Int("round", round),
				zap.Duration("total_duration", iterResult.TotalDuration),
			)
			return iterResult, nil
		}

		// Prepare for next round: use new files and new analysis as feedback
		if fixRound.Files != nil {
			currentFiles = fixRound.Files
		}
		currentAnalysis = fixRound.Analysis
		currentFeedback = nil // Analysis failures replace reviewer feedback in subsequent rounds
	}

	// All fix rounds exhausted
	iterResult.FinalRound = len(iterResult.Rounds) + 1
	iterResult.FinalPassed = false
	iterResult.TotalDuration = time.Since(startTime)

	ie.logger.Warn("fix loop exhausted all rounds",
		zap.String("proposal_id", prop.ID),
		zap.Int("rounds_attempted", len(iterResult.Rounds)),
		zap.Duration("total_duration", iterResult.TotalDuration),
	)

	return iterResult, fmt.Errorf("fix loop exhausted %d rounds without passing", MaxFixRounds)
}

// runSingleFixRound executes one iteration: re-generate code → re-commit → re-analyze.
func (ie *IterationEngine) runSingleFixRound(
	ctx context.Context,
	builderID string,
	prop *proposal.Proposal,
	spec *SkillSpec,
	fixReq *FixRequest,
	repoKind gitmanager.RepoKind,
) (*FixRound, error) {
	fr := &FixRound{
		Round:        fixReq.Round,
		State:        FixRoundRunning,
		FeedbackUsed: fixReq.Feedback,
		StartedAt:    time.Now().UTC(),
	}

	// Step 1: Build feedback-enriched prompt for CodeGenerator
	feedbackSummary := fixReq.FeedbackSummary()
	feedbackLines := strings.Split(feedbackSummary, "\n")
	feedbackSlice := make([]string, 0, len(feedbackLines))
	for _, line := range feedbackLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			feedbackSlice = append(feedbackSlice, trimmed)
		}
	}

	// Use the skill_fix template
	tmpl, ok := ie.codeGen.GetTemplate("skill_fix")
	if !ok {
		return fr, fmt.Errorf("skill_fix template not found")
	}

	specJSON, _ := json.Marshal(spec)
	systemPrompt, _ := tmpl.Format(map[string]string{
		"skill_spec": string(specJSON),
	})

	codeReq := &CodeGenRequest{
		Spec:         *spec,
		ExistingCode: fixReq.CurrentFiles,
		Feedback:     feedbackSlice,
		Round:        fixReq.Round,
		SystemPrompt: systemPrompt,
		MaxTokens:    8192,
	}

	// Step 2: Re-generate code
	codeResp, err := ie.codeGen.Generate(builderID, codeReq)
	if err != nil {
		return fr, fmt.Errorf("code generation failed in fix round %d: %w", fixReq.Round, err)
	}
	fr.Files = codeResp.Files

	// Step 3: Commit to the existing proposal branch
	commitMsg := fmt.Sprintf("fix(%s): iteration round %d\n\nProposal: %s\nFix round addressing reviewer feedback",
		spec.Name, fixReq.Round, prop.ID)

	commitHash, err := ie.gitMgr.CommitFiles(repoKind, codeResp.Files, commitMsg)
	if err != nil {
		return fr, fmt.Errorf("failed to commit fix round %d: %w", fixReq.Round, err)
	}
	fr.CommitHash = commitHash

	// Step 4: Generate updated diff
	diff, err := ie.gitMgr.GenerateDiff(repoKind, prop.ID)
	if err != nil {
		ie.logger.Warn("failed to generate diff for fix round",
			zap.Int("round", fixReq.Round),
			zap.Error(err),
		)
		diff = "(diff generation failed)"
	}
	fr.Diff = diff

	// Step 5: Re-run analysis
	if ie.analyzer != nil {
		analysisReq := &AnalysisRequest{
			ProposalID: prop.ID,
			Files:      codeResp.Files,
			Diff:       diff,
			SkillName:  spec.Name,
		}

		analysisResult, analysisErr := ie.analyzer.Analyze(builderID, analysisReq)
		if analysisErr != nil {
			ie.logger.Error("analysis failed in fix round",
				zap.Int("round", fixReq.Round),
				zap.Error(analysisErr),
			)
		} else {
			fr.Analysis = analysisResult
		}
	}

	fr.State = FixRoundComplete
	fr.CompletedAt = time.Now().UTC()
	fr.Duration = fr.CompletedAt.Sub(fr.StartedAt)

	ie.logger.Info("fix round complete",
		zap.String("proposal_id", prop.ID),
		zap.Int("round", fixReq.Round),
		zap.String("commit", commitHash),
		zap.Bool("analysis_passed", fr.Analysis != nil && fr.Analysis.Passed),
		zap.Duration("duration", fr.Duration),
	)

	return fr, nil
}

// logFixRoundAudit records a fix round in the audit log.
func (ie *IterationEngine) logFixRoundAudit(proposalID string, round int, passed bool, errMsg string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": proposalID,
		"fix_round":   round,
		"passed":      passed,
		"error":       errMsg,
	})

	action := kernel.NewAction(kernel.ActionBuilderBuild, "iteration-engine", payload)
	if _, logErr := ie.kern.SignAndLog(action); logErr != nil {
		ie.logger.Error("failed to log fix round audit", zap.Error(logErr))
	}
}

// ExtractFeedback converts Court reviews into structured ReviewFeedback for the fix loop.
func ExtractFeedback(reviews []proposal.Review) []ReviewFeedback {
	var feedback []ReviewFeedback
	for _, r := range reviews {
		if r.Verdict != proposal.VerdictReject && r.Verdict != proposal.VerdictAsk {
			continue
		}

		fb := ReviewFeedback{
			ReviewerPersona: r.Persona,
			Verdict:         string(r.Verdict),
			Comments:        r.Comments,
			Questions:       r.Questions,
		}

		// Extract concerns from evidence with negative connotation
		for _, e := range r.Evidence {
			lower := strings.ToLower(e)
			if strings.Contains(lower, "concern") || strings.Contains(lower, "risk") ||
				strings.Contains(lower, "vulnerable") || strings.Contains(lower, "unsafe") ||
				strings.Contains(lower, "insecure") || strings.Contains(lower, "missing") {
				fb.Concerns = append(fb.Concerns, e)
			}
		}

		feedback = append(feedback, fb)
	}
	return feedback
}

// GetIteration returns the iteration result for a proposal.
func (ie *IterationEngine) GetIteration(proposalID string) (*IterationResult, bool) {
	ie.mu.Lock()
	defer ie.mu.Unlock()
	r, ok := ie.iterations[proposalID]
	return r, ok
}

// ListIterations returns all iteration results.
func (ie *IterationEngine) ListIterations() []*IterationResult {
	ie.mu.Lock()
	defer ie.mu.Unlock()

	results := make([]*IterationResult, 0, len(ie.iterations))
	for _, r := range ie.iterations {
		results = append(results, r)
	}
	return results
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'builder', 'iteration.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"iteration.go: {len(code)} bytes -> {outpath}")
