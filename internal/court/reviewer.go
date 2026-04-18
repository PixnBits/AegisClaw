package court

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
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
	Temperature *float64        `json:"temperature,omitempty"`
	Seed        int64           `json:"seed,omitempty"`
}

// ReviewResponse is the structured JSON response expected from a reviewer sandbox.
type ReviewResponse struct {
	Verdict   string   `json:"verdict"`
	RiskScore float64  `json:"risk_score"`
	Evidence  []string `json:"evidence"`
	Questions []string `json:"questions,omitempty"`
	Comments  string   `json:"comments"`
}

// UnmarshalJSON handles LLMs returning evidence as a string instead of an array.
func (rr *ReviewResponse) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion.
	type Alias ReviewResponse
	aux := &struct {
		Evidence json.RawMessage `json:"evidence"`
		*Alias
	}{
		Alias: (*Alias)(rr),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.Evidence) == 0 {
		rr.Evidence = nil
		return nil
	}

	// Try array first.
	var arr []string
	if err := json.Unmarshal(aux.Evidence, &arr); err == nil {
		rr.Evidence = arr
		return nil
	}

	// Fall back to a single string.
	var s string
	if err := json.Unmarshal(aux.Evidence, &s); err == nil {
		if s != "" {
			rr.Evidence = []string{s}
		}
		return nil
	}

	return fmt.Errorf("evidence must be a string or array of strings")
}

// Validate checks the response has valid fields per the PRD schema requirements.
// This implements the schema validation gate (D7) that ensures 98%+ structured
// JSON success rate across all Court interactions.
func (rr *ReviewResponse) Validate() error {
	switch proposal.ReviewVerdict(rr.Verdict) {
	case proposal.VerdictApprove, proposal.VerdictReject, proposal.VerdictAsk, proposal.VerdictAbstain:
	default:
		return fmt.Errorf("invalid verdict: %q (must be approve, reject, ask, or abstain)", rr.Verdict)
	}
	if rr.RiskScore < 0 || rr.RiskScore > 10 {
		return fmt.Errorf("risk score must be between 0 and 10, got %f", rr.RiskScore)
	}
	if rr.Comments == "" {
		return fmt.Errorf("comments are required for all review responses")
	}
	if len(rr.Evidence) == 0 && rr.Verdict != string(proposal.VerdictAbstain) {
		return fmt.Errorf("evidence is required for non-abstain verdicts")
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
	config  sandbox.RuntimeConfig
	proxy   *llm.OllamaProxy
	logger  *zap.Logger
}

// NewFirecrackerLauncher creates a real launcher for reviewer sandboxes.
// proxy must not be nil; it is started per-VM after each reviewer VM boots.
func NewFirecrackerLauncher(runtime *sandbox.FirecrackerRuntime, cfg sandbox.RuntimeConfig, proxy *llm.OllamaProxy, logger *zap.Logger) *FirecrackerLauncher {
	return &FirecrackerLauncher{
		runtime: runtime,
		config:  cfg,
		proxy:   proxy,
		logger:  logger,
	}
}

// LaunchReviewer creates and starts a Firecracker reviewer sandbox.
// The sandbox has NO network interface; all LLM access goes through the
// host-side OllamaProxy over vsock (no TAP device, no IP stack in the VM).
func (fl *FirecrackerLauncher) LaunchReviewer(ctx context.Context, persona *Persona, model string) (string, error) {
	sandboxID := "reviewer-" + uuid.New().String()[:8]
	// Sanitize model name for sandbox naming (colons are invalid in sandbox names).
	safeName := strings.ReplaceAll(model, ":", "-")
	spec := sandbox.SandboxSpec{
		ID:   sandboxID,
		Name: fmt.Sprintf("reviewer-%s-%s", persona.Name, safeName),
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 512,
		},
		// NoNetwork: reviewer VMs reach Ollama exclusively through the host-side
		// LLM proxy over vsock.  No TAP device means no IP stack in the VM.
		NetworkPolicy: sandbox.NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
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

	// Start the per-VM LLM proxy listener.  The proxy binds to
	// <vsock_path>_1025 which Firecracker routes guest vsock connections to.
	vsockPath, err := fl.runtime.VsockPath(sandboxID)
	if err != nil {
		fl.runtime.Stop(ctx, sandboxID)
		fl.runtime.Delete(ctx, sandboxID)
		return "", fmt.Errorf("failed to get vsock path for reviewer sandbox: %w", err)
	}
	if err := fl.proxy.StartForVM(sandboxID, vsockPath); err != nil {
		fl.runtime.Stop(ctx, sandboxID)
		fl.runtime.Delete(ctx, sandboxID)
		return "", fmt.Errorf("failed to start llm proxy for reviewer sandbox: %w", err)
	}

	fl.logger.Info("reviewer sandbox launched",
		zap.String("sandbox_id", sandboxID),
		zap.String("persona", persona.Name),
		zap.String("model", model),
	)
	return sandboxID, nil
}

// SendReviewRequest sends the review prompt to the reviewer sandbox via vsock
// using the Firecracker vsock proxy protocol (CONNECT <port>\n) and waits for
// the structured verdict response.
func (fl *FirecrackerLauncher) SendReviewRequest(ctx context.Context, sandboxID string, req *ReviewRequest) (*ReviewResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal review request: %w", err)
	}

	ctlMsg := kernel.ControlMessage{
		Type:    "review.execute",
		Payload: payload,
	}

	// Use SendToVM which speaks the Firecracker vsock CONNECT protocol
	// (CONNECT 1024\n → OK 1024\n), falling back to TCP via guest IP.
	rawResp, err := fl.runtime.SendToVM(ctx, sandboxID, ctlMsg)
	if err != nil {
		return nil, fmt.Errorf("send to reviewer vm failed: %w", err)
	}

	var guestResp struct {
		Success bool            `json:"success"`
		Error   string          `json:"error,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(rawResp, &guestResp); err != nil {
		return nil, fmt.Errorf("parse reviewer response envelope: %w", err)
	}
	if !guestResp.Success {
		return nil, fmt.Errorf("reviewer returned error: %s", guestResp.Error)
	}

	var reviewResp ReviewResponse
	if err := json.Unmarshal(guestResp.Data, &reviewResp); err != nil {
		return nil, fmt.Errorf("failed to parse review response: %w", err)
	}
	if err := reviewResp.Validate(); err != nil {
		return nil, fmt.Errorf("invalid review response: %w", err)
	}

	return &reviewResp, nil
}

// StopReviewer stops and deletes a reviewer sandbox and closes its LLM proxy.
func (fl *FirecrackerLauncher) StopReviewer(ctx context.Context, sandboxID string) error {
	fl.proxy.StopForVM(sandboxID)
	if err := fl.runtime.Stop(ctx, sandboxID); err != nil {
		fl.logger.Error("failed to stop reviewer sandbox", zap.String("id", sandboxID), zap.Error(err))
	}
	return fl.runtime.Delete(ctx, sandboxID)
}

// Reviewer manages the execution of a single persona review with cross-verification.
type Reviewer struct {
	launcher    SandboxLauncher
	minModels   int
	temperature *float64
	seed        int64
	logger      *zap.Logger
}

type modelResult struct {
	model    string
	response *ReviewResponse
	err      error
}

// NewReviewer creates a Reviewer that requires cross-verification across minModels models.
func NewReviewer(launcher SandboxLauncher, minModels int, logger *zap.Logger) *Reviewer {
	return NewReviewerWithLLMOptions(launcher, minModels, logger, nil, 0)
}

// NewReviewerWithLLMOptions applies deterministic LLM settings when tests need
// reproducible reviewer calls without changing normal runtime behavior.
func NewReviewerWithLLMOptions(launcher SandboxLauncher, minModels int, logger *zap.Logger, temperature *float64, seed int64) *Reviewer {
	if minModels < 1 {
		minModels = 2
	}
	return &Reviewer{
		launcher:    launcher,
		minModels:   minModels,
		temperature: temperature,
		seed:        seed,
		logger:      logger,
	}
}

// Execute runs a single persona review, cross-verifying across multiple models.
// Returns the aggregated review.
func (r *Reviewer) Execute(ctx context.Context, p *proposal.Proposal, persona *Persona) (*proposal.Review, error) {
	models := persona.Models
	if len(models) == 0 {
		return nil, fmt.Errorf("persona %s has no models configured", persona.Name)
	}
	if len(models) < r.minModels {
		r.logger.Warn("persona has fewer models than required for cross-verification; proceeding without cross-verification",
			zap.String("persona", persona.Name),
			zap.Int("models", len(models)),
			zap.Int("min_models", r.minModels),
		)
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
		Temperature: r.temperature,
		Seed:        r.seed,
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
