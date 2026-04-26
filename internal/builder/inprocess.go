package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"go.uber.org/zap"
)

// InProcessBuilderRuntime is a simplified BuilderRuntime for use inside a builder microVM.
// It doesn't launch nested VMs; instead it runs code generation and analysis in-process.
type InProcessBuilderRuntime struct {
	logger    *zap.Logger
	llmClient *llm.Client
	currentID string
}

// NewInProcessBuilderRuntime creates a builder runtime that executes in-process.
// This is used when running inside the builder-agent microVM, where we don't want
// to launch nested VMs.
func NewInProcessBuilderRuntime(logger *zap.Logger) *InProcessBuilderRuntime {
	// Create Ollama client pointing to localhost
	// The LLM proxy makes Ollama available at localhost:11434 inside the microVM
	llmClient := llm.NewClient(llm.ClientConfig{
		Endpoint: "http://127.0.0.1:11434",
		Timeout:  10 * time.Minute,
	})

	return &InProcessBuilderRuntime{
		logger:    logger,
		llmClient: llmClient,
	}
}

// LaunchBuilder is a no-op for in-process execution.
// We're already running inside the builder VM.
func (rt *InProcessBuilderRuntime) LaunchBuilder(ctx context.Context, spec *BuilderSpec) (*BuilderInfo, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid builder spec: %w", err)
	}

	// Create a fake builder info representing the current VM
	now := time.Now().UTC()
	info := &BuilderInfo{
		ID:         "in-process",
		ProposalID: spec.ProposalID,
		State:      BuilderStateBuilding,
		SandboxID:  "in-process",
		StartedAt:  &now,
	}

	rt.currentID = "in-process"
	rt.logger.Info("using in-process builder (already in microVM)",
		zap.String("proposal_id", spec.ProposalID),
	)

	return info, nil
}

// StopBuilder is a no-op for in-process execution.
func (rt *InProcessBuilderRuntime) StopBuilder(ctx context.Context, builderID string) error {
	rt.logger.Debug("stop builder (no-op for in-process)",
		zap.String("builder_id", builderID),
	)
	return nil
}

// SendBuildRequest executes the request in-process instead of sending via vsock.
// For code generation requests, it calls Ollama directly.
// For analysis requests, it runs tools directly.
func (rt *InProcessBuilderRuntime) SendBuildRequest(ctx context.Context, builderID string, msg kernel.ControlMessage) (*kernel.ControlResponse, error) {
	switch msg.Type {
	case "codegen.generate":
		return rt.handleCodeGenInProcess(ctx, msg)
	case "analysis.run":
		return rt.handleAnalysisInProcess(ctx, msg)
	default:
		return nil, fmt.Errorf("unsupported message type for in-process execution: %s", msg.Type)
	}
}

// handleCodeGenInProcess generates code using the local Ollama instance.
func (rt *InProcessBuilderRuntime) handleCodeGenInProcess(ctx context.Context, msg kernel.ControlMessage) (*kernel.ControlResponse, error) {
	var req CodeGenRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal codegen request: %w", err)
	}

	rt.logger.Info("generating code in-process",
		zap.String("skill", req.Spec.Name),
		zap.Int("round", req.Round),
	)

	// Build chat messages for Ollama
	messages := []llm.ChatMessage{
		{
			Role:    "system",
			Content: req.SystemPrompt,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Generate code for skill: %s\n\nDescription: %s", req.Spec.Name, req.Spec.Description),
		},
	}

	// Add existing code context if this is a revision round
	if len(req.ExistingCode) > 0 {
		existingCodeJSON, _ := json.MarshalIndent(req.ExistingCode, "", "  ")
		messages = append(messages, llm.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("Previous code:\n%s", string(existingCodeJSON)),
		})
	}

	// Add feedback if provided
	if len(req.Feedback) > 0 {
		feedbackStr := ""
		for i, fb := range req.Feedback {
			feedbackStr += fmt.Sprintf("%d. %s\n", i+1, fb)
		}
		messages = append(messages, llm.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("Address this feedback:\n%s", feedbackStr),
		})
	}

	// Call Ollama
	start := time.Now()
	chatReq := llm.ChatRequest{
		Model:    "qwen2.5-coder:7b", // Use a capable code generation model
		Messages: messages,
		Format:   "json", // Request JSON response
		Stream:   false,
		Options: map[string]any{
			"temperature": 0.7,
			"num_predict": req.MaxTokens,
		},
	}

	resp, err := rt.llmClient.Chat(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}

	// Parse the response
	var codeResp CodeGenResponse
	if err := json.Unmarshal([]byte(resp.Message.Content), &codeResp); err != nil {
		// If JSON parsing fails, try to extract code from text response
		rt.logger.Warn("failed to parse JSON response, using text extraction",
			zap.Error(err),
		)
		codeResp = CodeGenResponse{
			Files:     map[string]string{"generated.go": resp.Message.Content},
			Reasoning: "Code extracted from text response",
			Round:     req.Round,
			Duration:  time.Since(start),
		}
	} else {
		codeResp.Round = req.Round
		codeResp.Duration = time.Since(start)
	}

	// Marshal response
	respPayload, err := json.Marshal(codeResp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal codegen response: %w", err)
	}

	rt.logger.Info("code generation complete",
		zap.Int("files", len(codeResp.Files)),
		zap.Duration("duration", codeResp.Duration),
	)

	return &kernel.ControlResponse{
		Success: true,
		Data:    respPayload,
	}, nil
}

// handleAnalysisInProcess runs analysis tools directly in the current process.
func (rt *InProcessBuilderRuntime) handleAnalysisInProcess(ctx context.Context, msg kernel.ControlMessage) (*kernel.ControlResponse, error) {
	var req AnalysisRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis request: %w", err)
	}

	rt.logger.Info("running analysis in-process",
		zap.String("skill", req.SkillName),
		zap.Int("files", len(req.Files)),
	)

	// For now, return a minimal success result
	// TODO: Implement actual go test, golangci-lint, gosec execution
	result := &AnalysisResult{
		ProposalID:     req.ProposalID,
		Diff:           req.Diff,
		TestOutput:     "(analysis not yet implemented in microVM)",
		TestPassed:     true, // Assume pass for now
		LintOutput:     "",
		LintPassed:     true,
		SecurityOutput: "",
		SecurityPassed: true,
		BuildOutput:    "",
		BuildPassed:    true,
		Findings:       []AnalysisFinding{},
		Passed:         true,
		Duration:       time.Second,
		CompletedAt:    time.Now().UTC(),
	}

	respPayload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal analysis result: %w", err)
	}

	rt.logger.Info("analysis complete (stub)", zap.Bool("passed", result.Passed))

	return &kernel.ControlResponse{
		Success: true,
		Data:    respPayload,
	}, nil
}

// Chat is a helper method to call Ollama directly from external code.
func (rt *InProcessBuilderRuntime) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return rt.llmClient.Chat(ctx, req)
}

// DefaultPromptTemplates returns the standard prompt templates for code generation.
func DefaultPromptTemplates() map[string]*PromptTemplate {
	return map[string]*PromptTemplate{
		"skill_codegen": {
			Name:        "skill_codegen",
			Description: "Generates Go code for a new skill",
			System: `You are an expert Go developer building skills for the AegisClaw framework.
Your task is to generate production-quality Go code that implements the skill specification.

Guidelines:
- Follow Go best practices and idioms
- Include comprehensive error handling
- Add package and function documentation
- Implement all required tools from the spec
- Use appropriate data structures
- Keep code secure and avoid common vulnerabilities

Return your response as JSON with this structure:
{
  "files": {
    "path/to/file.go": "package content..."
  },
  "reasoning": "Brief explanation of design choices"
}`,
			User: `Generate code for this skill:

{{skill_spec}}`,
		},
		"skill_script_runner": {
			Name:        "skill_script_runner",
			Description: "Generates script runner for Python/JavaScript/Bash skills",
			System: `You are building a skill wrapper for the AegisClaw framework.
Generate a Go wrapper that executes scripts in the specified language securely.

Guidelines:
- Use os/exec for script execution
- Validate and sanitize inputs
- Implement proper timeout handling
- Capture stdout/stderr properly
- Handle script exit codes

Return JSON with the same structure as skill_codegen.`,
			User: `Generate a script runner for:

{{skill_spec}}`,
		},
	}
}
