package court

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ReviewRequest is the payload sent to each reviewer sandbox via vsock.
type ReviewRequest struct {
	ProposalID  string          `json:"proposal_id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Spec        json.RawMessage `json:"spec,omitempty"`
	PersonaName string          `json:"persona_name"`
	PersonaRole string          `json:"persona_role"`
	Prompt      string          `json:"prompt"`
	Model       string          `json:"model"`
	Round       int             `json:"round"`
}

// ReviewResponse is the structured JSON response expected from a reviewer sandbox.
type ReviewResponse struct {
	Verdict   string   `json:"verdict"`
	RiskScore float64  `json:"risk_score"`
	Evidence  []string `json:"evidence"`
	Questions []string `json:"questions,omitempty"`
	Comments  string   `json:"comments"`
}

// Validate checks the response has valid fields.
func (rr *ReviewResponse) Validate() error {
	switch proposal.ReviewVerdict(rr.Verdict) {
	case proposal.VerdictApprove, proposal.VerdictReject, proposal.VerdictAsk, proposal.VerdictAbstain:
	default:
		return fmt.Errorf("invalid verdict: %q", rr.Verdict)
	}
	if rr.RiskScore < 0 || rr.RiskScore > 10 {
		return fmt.Errorf("risk score must be between 0 and 10, got %f", rr.RiskScore)
	}
	if len(rr.Evidence) == 0 {
		return fmt.Errorf("at least one evidence item is required")
	}
	return nil
}

// SandboxLauncher abstracts sandbox creation/destruction for testing.
type SandboxLauncher interface {
	LaunchReviewer(ctx context.Context, persona *Persona, model string) (string, error)
	SendReviewRequest(ctx context.Context, sandboxID string, req *ReviewRequest) (*ReviewResponse, error)
	StopReviewer(ctx context.Context, sandboxID string) error
}

// FirecrackerLauncher uses real Firecracker sandboxes for reviewer execution.
type FirecrackerLauncher struct {
	runtime *sandbox.FirecrackerRuntime
	kern    *kernel.Kernel
	config  sandbox.RuntimeConfig
	logger  *zap.Logger
}

// NewFirecrackerLauncher creates a real launcher for reviewer sandboxes.
func NewFirecrackerLauncher(runtime *sandbox.FirecrackerRuntime, kern *kernel.Kernel, cfg sandbox.RuntimeConfig, logger *zap.Logger) *FirecrackerLauncher {
	return &FirecrackerLauncher{
		runtime: runtime,
		kern:    kern,
		config:  cfg,
		logger:  logger,
	}
}

// LaunchReviewer creates and starts a Firecracker reviewer sandbox.
func (fl *FirecrackerLauncher) LaunchReviewer(ctx context.Context, persona *Persona, model string) (string, error) {
	sandboxID := uuid.New().String()
	spec := sandbox.SandboxSpec{
		ID:   sandboxID,
		Name: fmt.Sprintf("reviewer-%s-%s", persona.Name, model),
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 512,
		},
		NetworkPolicy: sandbox.NetworkPolicy{
			DefaultDeny:  true,
			AllowedHosts: []string{"127.0.0.1"},
			AllowedPorts: []uint16{11434},
		},
		RootfsPath: fl.config.RootfsTemplate,
	}

	if err := fl.runtime.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("failed to create reviewer sandbox: %w", err)
	}
	if err := fl.runtime.Start(ctx, sandboxID); err != nil {
		fl.runtime.Delete(ctx, sandboxID)
		return "", fmt.Errorf("failed to start reviewer sandbox: %w", err)
	}

	fl.logger.Info("reviewer sandbox launched",
		zap.String("sandbox_id", sandboxID),
		zap.String("persona", persona.Name),
		zap.String("model", model),
	)
	return sandboxID, nil
}

// SendReviewRequest sends the review prompt via the kernel control plane (vsock).
func (fl *FirecrackerLauncher) SendReviewRequest(ctx context.Context, sandboxID string, req *ReviewRequest) (*ReviewResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal review request: %w", err)
	}

	ctlMsg := kernel.ControlMessage{
		Type:    "review.execute",
		Payload: payload,
	}

	resp, err := fl.kern.ControlPlane().Send(sandboxID, ctlMsg)
	if err != nil {
		return nil, fmt.Errorf("vsock send failed: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("reviewer returned error: %s", resp.Error)
	}

	var reviewResp ReviewResponse
	if err := json.Unmarshal(resp.Data, &reviewResp); err != nil {
		return nil, fmt.Errorf("failed to parse review response: %w", err)
	}

	if err := reviewResp.Validate(); err != nil {
		return nil, fmt.Errorf("invalid review response: %w", err)
	}

	return &reviewResp, nil
}

// StopReviewer stops and deletes a reviewer sandbox.
func (fl *FirecrackerLauncher) StopReviewer(ctx context.Context, sandboxID string) error {
	if err := fl.runtime.Stop(ctx, sandboxID); err != nil {
		fl.logger.Error("failed to stop reviewer sandbox", zap.String("id", sandboxID), zap.Error(err))
	}
	return fl.runtime.Delete(ctx, sandboxID)
}

// Reviewer manages the execution of a single persona review with cross-verification.
type Reviewer struct {
	launcher  SandboxLauncher
	minModels int
	logger    *zap.Logger
}

type modelResult struct {
	model    string
	response *ReviewResponse
	err      error
}

// NewReviewer creates a Reviewer that requires cross-verification across minModels models.
func NewReviewer(launcher SandboxLauncher, minModels int, logger *zap.Logger) *Reviewer {
	if minModels < 1 {
		minModels = 2
	}
	return &Reviewer{
		launcher:  launcher,
		minModels: minModels,
		logger:    logger,
	}
}

// Execute runs a single persona review, cross-verifying across multiple models.
// Returns the aggregated review.
func (r *Reviewer) Execute(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
	models := persona.Models
	if len(models) < r.minModels {
		return nil, fmt.Errorf("persona %s has %d models but %d required for cross-verification", persona.Name, len(models), r.minModels)
	}

	results := make(chan modelResult, len(models))
	var wg sync.WaitGroup

	for _, model := range models {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			resp, err := r.runSingleModel(ctx, p, persona, m)
			results <- modelResult{model: m, response: resp, err: err}
		}(model)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var responses []modelResult
	for res := range results {
		if res.err != nil {
			r.logger.Error("model review failed",
				zap.String("persona", persona.Name),
				zap.String("model", res.model),
				zap.Error(res.err),
			)
			continue
		}
		responses = append(responses, res)
	}

	if len(responses) == 0 {
		return nil, fmt.Errorf("all model reviews failed for persona %s", persona.Name)
	}

	// Cross-verify: check that models agree on verdict
	aggregated := r.crossVerify(responses, persona)

	review := &proposal.Review{
		ID:        uuid.New().String(),
		Persona:   persona.Name,
		Model:     r.modelNames(responses),
		Round:     p.Round,
		Verdict:   aggregated.Verdict,
		RiskScore: aggregated.RiskScore,
		Evidence:  aggregated.Evidence,
		Questions: aggregated.Questions,
		Comments:  aggregated.Comments,
		Timestamp: time.Now().UTC(),
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"model_responses": len(responses),
		"cross_verified":  len(responses) >= r.minModels,
	})
	review.Raw = raw

	r.logger.Info("review complete",
		zap.String("persona", persona.Name),
		zap.String("verdict", string(aggregated.Verdict)),
		zap.Float64("risk_score", aggregated.RiskScore),
		zap.Int("models_used", len(responses)),
	)

	return review, nil
}

func (r *Reviewer) runSingleModel(ctx context.Context, p *proposal.Proposal, persona *Persona, model string) (*ReviewResponse, error) {
	sandboxID, err := r.launcher.LaunchReviewer(ctx, persona, model)
	if err != nil {
		return nil, err
	}
	defer r.launcher.StopReviewer(ctx, sandboxID)

	req := &ReviewRequest{
		ProposalID:  p.ID,
		Title:       p.Title,
		Description: p.Description,
		Category:    string(p.Category),
		Spec:        p.Spec,
		PersonaName: persona.Name,
		PersonaRole: persona.Role,
		Prompt:      persona.SystemPrompt,
		Model:       model,
		Round:       p.Round,
	}

	return r.launcher.SendReviewRequest(ctx, sandboxID, req)
}

// crossVerify aggregates multiple model responses, checking for agreement.
func (r *Reviewer) crossVerify(results []modelResult, persona *Persona) *aggregatedReview {
	verdictCounts := make(map[proposal.ReviewVerdict]int)
	var totalRisk float64
	var allEvidence []string
	var allQuestions []string
	var allComments []string

	for _, res := range results {
		verdict := proposal.ReviewVerdict(res.response.Verdict)
		verdictCounts[verdict]++
		totalRisk += res.response.RiskScore
		allEvidence = append(allEvidence, res.response.Evidence...)
		allQuestions = append(allQuestions, res.response.Questions...)
		if res.response.Comments != "" {
			allComments = append(allComments, fmt.Sprintf("[%s] %s", res.model, res.response.Comments))
		}
	}

	// Determine majority verdict
	majorityVerdict := proposal.VerdictAbstain
	maxCount := 0
	for v, count := range verdictCounts {
		if count > maxCount {
			maxCount = count
			majorityVerdict = v
		}
	}

	// If models disagree, escalate to "ask" for human review
	if len(verdictCounts) > 1 && maxCount < len(results) {
		r.logger.Warn("model disagreement detected",
			zap.String("persona", persona.Name),
			zap.Any("verdicts", verdictCounts),
		)
		// If there is any rejection, lean toward rejection for safety
		if verdictCounts[proposal.VerdictReject] > 0 {
			majorityVerdict = proposal.VerdictReject
		} else {
			majorityVerdict = proposal.VerdictAsk
		}
	}

	avgRisk := totalRisk / float64(len(results))

	// Deduplicate evidence
	evidenceSet := make(map[string]bool)
	var uniqueEvidence []string
	for _, e := range allEvidence {
		if !evidenceSet[e] {
			evidenceSet[e] = true
			uniqueEvidence = append(uniqueEvidence, e)
		}
	}

	comments := ""
	for _, c := range allComments {
		if comments != "" {
			comments += " | "
		}
		comments += c
	}

	return &aggregatedReview{
		Verdict:   majorityVerdict,
		RiskScore: avgRisk,
		Evidence:  uniqueEvidence,
		Questions: allQuestions,
		Comments:  comments,
	}
}

type aggregatedReview struct {
	Verdict   proposal.ReviewVerdict
	RiskScore float64
	Evidence  []string
	Questions []string
	Comments  string
}

func (r *Reviewer) modelNames(results []modelResult) string {
	names := ""
	for _, res := range results {
		if names != "" {
			names += ","
		}
		names += res.model
	}
	return names
}

// NewReviewerFunc creates a ReviewerFunc from a Reviewer instance (adapter for the Engine).
func NewReviewerFunc(reviewer *Reviewer) ReviewerFunc {
	return func(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
		return reviewer.Execute(ctx, p, persona)
	}
}
