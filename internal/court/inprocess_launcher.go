//go:build inprocesstest
// +build inprocesstest

// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  WARNING — TEST-ONLY CODE — NOT FOR PRODUCTION USE                      ║
// ║                                                                          ║
// ║  This file is compiled ONLY when the "inprocesstest" build tag is set.  ║
// ║  It MUST NOT appear in any production binary, release build, or         ║
// ║  default "go test ./..." run.                                            ║
// ║                                                                          ║
// ║  InProcessSandboxLauncher has ZERO isolation: it runs reviewer LLM      ║
// ║  inference directly in the test process with no Firecracker microVM,    ║
// ║  no jailer, and no capability dropping. There is NO security boundary.  ║
// ╚══════════════════════════════════════════════════════════════════════════╝

package court

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Safety guard constants — must match InProcessTaskExecutor in
// internal/runtime/exec/inprocess_executor.go.
const (
	inProcessEnvVar   = "AEGISCLAW_INPROCESS_TEST_MODE"
	inProcessEnvValue = "unsafe_for_testing_only"
)

// InProcessSandboxLauncher implements SandboxLauncher entirely inside the
// current process — no Firecracker, no jailer, no vsock.
//
// SECURITY: This launcher provides ZERO isolation. It is exclusively for
// fast in-process integration testing. Production builds cannot contain this
// code because it is compiled only under the "inprocesstest" build tag.
//
// Safety guards (both must pass at construction time):
//  1. AEGISCLAW_INPROCESS_TEST_MODE must equal "unsafe_for_testing_only".
//  2. A loud, multi-line warning is logged on every instantiation.
type InProcessSandboxLauncher struct {
	proxy  *llm.OllamaProxy
	logger *zap.Logger
}

// NewInProcessSandboxLauncher creates an InProcessSandboxLauncher backed by proxy.
//
// It panics immediately if AEGISCLAW_INPROCESS_TEST_MODE is not set to the
// required sentinel value. This is intentional: the panic makes it impossible
// to accidentally use this launcher in an environment where the variable is
// not set.
func NewInProcessSandboxLauncher(proxy *llm.OllamaProxy, logger *zap.Logger) *InProcessSandboxLauncher {
	if os.Getenv(inProcessEnvVar) != inProcessEnvValue {
		panic(fmt.Sprintf(
			"NewInProcessSandboxLauncher: safety guard failed.\n"+
				"Set %s=%s to acknowledge that this launcher has NO isolation.\n"+
				"This launcher must NEVER be used outside of test code.",
			inProcessEnvVar, inProcessEnvValue,
		))
	}

	logger.Warn(
		"INPROCESS SANDBOX LAUNCHER ACTIVE — reviewer LLM inference runs directly in the test process with ZERO isolation",
		zap.String("security", "test-only"),
		zap.String("mode", "inprocesstest"),
	)

	return &InProcessSandboxLauncher{proxy: proxy, logger: logger}
}

// LaunchReviewer returns a synthetic sandbox ID; no actual sandbox is created.
func (l *InProcessSandboxLauncher) LaunchReviewer(_ context.Context, persona *Persona, model string) (string, error) {
	id := "inprocess-reviewer-" + uuid.New().String()[:8]
	l.logger.Info("inprocess launcher: synthetic reviewer sandbox",
		zap.String("id", id),
		zap.String("persona", persona.Name),
		zap.String("model", model),
	)
	return id, nil
}

// SendReviewRequest replicates the guest-agent's review.execute handler,
// calling LLM inference directly via OllamaProxy.InferDirect instead of vsock.
// This mirrors the logic in cmd/guest-agent/main.go:handleReviewExecute.
func (l *InProcessSandboxLauncher) SendReviewRequest(_ context.Context, sandboxID string, req *ReviewRequest) (*ReviewResponse, error) {
	// Build the review user message — mirrors handleReviewExecute in the
	// guest-agent so that the request bodies match the recorded cassette.
	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "Review the following proposal (round %d):\n\n", req.Round)
	fmt.Fprintf(&userMsg, "Proposal ID: %s\n", req.ProposalID)
	fmt.Fprintf(&userMsg, "Title: %s\n", req.Title)
	fmt.Fprintf(&userMsg, "Description: %s\n", req.Description)
	fmt.Fprintf(&userMsg, "Category: %s\n", req.Category)
	if len(req.Spec) > 0 {
		fmt.Fprintf(&userMsg, "Spec: %s\n", string(req.Spec))
	}
	userMsg.WriteString("\nRespond with a JSON object containing:\n")
	userMsg.WriteString(`- "verdict": one of "approve", "reject", "ask", "abstain"` + "\n")
	userMsg.WriteString(`- "risk_score": a number between 0 and 10` + "\n")
	userMsg.WriteString(`- "evidence": an array of strings supporting your verdict` + "\n")
	userMsg.WriteString(`- "questions": (optional) an array of follow-up questions` + "\n")
	userMsg.WriteString(`- "comments": a brief summary of your assessment` + "\n")

	// Build options matching the guest-agent's buildOllamaOptions logic:
	// start with the default temperature (0.3), then apply any override.
	options := map[string]interface{}{
		"temperature": 0.3,
	}
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.Seed != 0 {
		options["seed"] = req.Seed
	}

	proxyReq := &llm.ProxyRequest{
		RequestID: uuid.New().String(),
		Model:     req.Model,
		Messages: []map[string]string{
			{"role": "system", "content": req.Prompt},
			{"role": "user", "content": userMsg.String()},
		},
		Format:  "json",
		Options: options,
	}

	resp := l.proxy.InferDirect(sandboxID, proxyReq)
	if resp.Error != "" {
		return nil, fmt.Errorf("inprocess reviewer: inference error from model %s: %s", req.Model, resp.Error)
	}

	raw := strings.TrimSpace(resp.Content)
	if raw == "" {
		return nil, fmt.Errorf("inprocess reviewer: empty response from model %s", req.Model)
	}

	var reviewResp ReviewResponse
	if err := json.Unmarshal([]byte(raw), &reviewResp); err != nil {
		return nil, fmt.Errorf("inprocess reviewer: failed to parse review response from model %s: %w", req.Model, err)
	}
	if err := reviewResp.Validate(); err != nil {
		return nil, fmt.Errorf("inprocess reviewer: invalid review response from model %s: %w", req.Model, err)
	}

	l.logger.Info("inprocess reviewer: review complete",
		zap.String("sandbox_id", sandboxID),
		zap.String("model", req.Model),
		zap.String("verdict", reviewResp.Verdict),
		zap.Float64("risk_score", reviewResp.RiskScore),
	)
	return &reviewResp, nil
}

// StopReviewer is a no-op; there is no real sandbox to tear down.
func (l *InProcessSandboxLauncher) StopReviewer(_ context.Context, sandboxID string) error {
	l.logger.Info("inprocess launcher: stop synthetic reviewer sandbox (no-op)",
		zap.String("id", sandboxID),
	)
	return nil
}
