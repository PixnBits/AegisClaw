package court

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DirectLauncher implements SandboxLauncher by calling Ollama directly on the
// host.  No Firecracker sandbox is created—this is used when sandboxed review
// is not required (e.g. during first-run or on machines without KVM/Firecracker).
type DirectLauncher struct {
	client *llm.Client
	logger *zap.Logger
}

// NewDirectLauncher creates a launcher that calls Ollama on the host.
func NewDirectLauncher(client *llm.Client, logger *zap.Logger) *DirectLauncher {
	return &DirectLauncher{
		client: client,
		logger: logger,
	}
}

// LaunchReviewer returns a unique ID; no sandbox is created.
func (dl *DirectLauncher) LaunchReviewer(_ context.Context, persona *Persona, model string) (string, error) {
	id := uuid.New().String()
	dl.logger.Info("direct reviewer launched (no sandbox)",
		zap.String("id", id),
		zap.String("persona", persona.Name),
		zap.String("model", model),
	)
	return id, nil
}

// SendReviewRequest calls Ollama directly and parses the structured JSON response.
func (dl *DirectLauncher) SendReviewRequest(ctx context.Context, _ string, req *ReviewRequest) (*ReviewResponse, error) {
	userContent := formatReviewUserMessage(req)

	chatReq := llm.ChatRequest{
		Model: req.Model,
		Messages: []llm.ChatMessage{
			{Role: "system", Content: req.Prompt},
			{Role: "user", Content: userContent},
		},
		Format: "json",
		Options: map[string]any{
			"temperature": 0.3,
		},
	}

	resp, err := dl.client.Chat(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}

	raw := strings.TrimSpace(resp.Message.Content)
	if raw == "" {
		return nil, fmt.Errorf("empty response from model %s", req.Model)
	}

	var reviewResp ReviewResponse
	if err := json.Unmarshal([]byte(raw), &reviewResp); err != nil {
		return nil, fmt.Errorf("failed to parse review JSON from %s: %w\nraw: %s", req.Model, err, truncate(raw, 500))
	}

	if err := reviewResp.Validate(); err != nil {
		return nil, fmt.Errorf("invalid review from %s: %w", req.Model, err)
	}

	return &reviewResp, nil
}

// StopReviewer is a no-op for the direct launcher.
func (dl *DirectLauncher) StopReviewer(_ context.Context, _ string) error {
	return nil
}

func formatReviewUserMessage(req *ReviewRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review the following proposal (round %d):\n\n", req.Round)
	fmt.Fprintf(&b, "Proposal ID: %s\n", req.ProposalID)
	fmt.Fprintf(&b, "Title: %s\n", req.Title)
	fmt.Fprintf(&b, "Description: %s\n", req.Description)
	fmt.Fprintf(&b, "Category: %s\n", req.Category)
	if len(req.Spec) > 0 {
		fmt.Fprintf(&b, "Spec: %s\n", string(req.Spec))
	}
	b.WriteString("\nRespond with a JSON object containing:\n")
	b.WriteString(`- "verdict": one of "approve", "reject", "ask", "abstain"`)
	b.WriteString("\n")
	b.WriteString(`- "risk_score": a number between 0 and 10`)
	b.WriteString("\n")
	b.WriteString(`- "evidence": an array of strings supporting your verdict`)
	b.WriteString("\n")
	b.WriteString(`- "questions": (optional) an array of follow-up questions`)
	b.WriteString("\n")
	b.WriteString(`- "comments": a brief summary of your assessment`)
	b.WriteString("\n")
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
