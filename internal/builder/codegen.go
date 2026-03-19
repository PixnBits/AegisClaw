package builder

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// SkillSpec defines the full specification for a skill to be built.
type SkillSpec struct {
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	Tools               []ToolSpec         `json:"tools"`
	NetworkPolicy       SkillNetworkPolicy `json:"network_policy"`
	SecretsRefs         []string           `json:"secrets_refs,omitempty"`
	PersonaRequirements []string           `json:"persona_requirements,omitempty"`
	Language            string             `json:"language"`
	EntryPoint          string             `json:"entry_point"`
	Dependencies        []string           `json:"dependencies,omitempty"`
	TestRequirements    string             `json:"test_requirements,omitempty"`
}

// ToolSpec describes a tool/function the skill provides.
type ToolSpec struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	InputSchema  string `json:"input_schema"`
	OutputSchema string `json:"output_schema"`
}

// SkillNetworkPolicy defines network access for the skill at runtime.
type SkillNetworkPolicy struct {
	DefaultDeny      bool     `json:"default_deny"`
	AllowedHosts     []string `json:"allowed_hosts,omitempty"`
	AllowedPorts     []uint16 `json:"allowed_ports,omitempty"`
	AllowedProtocols []string `json:"allowed_protocols,omitempty"`
}

var skillNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,62}$`)
var skillSecretRefRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`)

// Validate checks the SkillSpec has all required fields.
func (ss *SkillSpec) Validate() error {
	if ss.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if !skillNameRegex.MatchString(ss.Name) {
		return fmt.Errorf("skill name must match %s, got %q", skillNameRegex.String(), ss.Name)
	}
	if ss.Description == "" {
		return fmt.Errorf("skill description is required")
	}
	if len(ss.Description) > 2048 {
		return fmt.Errorf("skill description must be <= 2048 chars, got %d", len(ss.Description))
	}
	if len(ss.Tools) == 0 {
		return fmt.Errorf("at least one tool is required")
	}
	for i, t := range ss.Tools {
		if t.Name == "" {
			return fmt.Errorf("tool[%d] name is required", i)
		}
		if t.Description == "" {
			return fmt.Errorf("tool[%d] description is required", i)
		}
	}
	if ss.Language == "" {
		ss.Language = "go"
	}
	if ss.Language != "go" {
		return fmt.Errorf("only Go language is supported, got %q", ss.Language)
	}
	if ss.EntryPoint == "" {
		return fmt.Errorf("entry point is required")
	}
	if !ss.NetworkPolicy.DefaultDeny {
		return fmt.Errorf("network policy default_deny must be true")
	}
	for i, ref := range ss.SecretsRefs {
		if !skillSecretRefRegex.MatchString(ref) {
			return fmt.Errorf("secrets_refs[%d] %q is not a valid secret name", i, ref)
		}
	}
	return nil
}

// CodeGenRequest is sent to the builder sandbox to generate code.
type CodeGenRequest struct {
	Spec         SkillSpec         `json:"spec"`
	ExistingCode map[string]string `json:"existing_code,omitempty"`
	Feedback     []string          `json:"feedback,omitempty"`
	Round        int               `json:"round"`
	SystemPrompt string            `json:"system_prompt"`
	MaxTokens    int               `json:"max_tokens"`
}

// CodeGenResponse contains the generated code files.
type CodeGenResponse struct {
	Files     map[string]string `json:"files"`
	Reasoning string            `json:"reasoning"`
	Round     int               `json:"round"`
	Duration  time.Duration     `json:"duration"`
}

// Validate checks the CodeGenRequest has required fields.
func (r *CodeGenRequest) Validate() error {
	if err := r.Spec.Validate(); err != nil {
		return fmt.Errorf("invalid skill spec: %w", err)
	}
	if r.Round < 1 || r.Round > 3 {
		return fmt.Errorf("round must be between 1 and 3, got %d", r.Round)
	}
	if r.SystemPrompt == "" {
		return fmt.Errorf("system prompt is required")
	}
	if r.MaxTokens < 1 {
		r.MaxTokens = 4096
	}
	return nil
}

// Validate checks the CodeGenResponse has at least one file.
func (r *CodeGenResponse) Validate() error {
	if len(r.Files) == 0 {
		return fmt.Errorf("no files generated")
	}
	for path, content := range r.Files {
		if path == "" {
			return fmt.Errorf("empty file path in generated files")
		}
		if strings.Contains(path, "..") {
			return fmt.Errorf("path traversal detected in generated file path: %q", path)
		}
		if content == "" {
			return fmt.Errorf("empty content for file %q", path)
		}
	}
	return nil
}

// PromptTemplate defines a reusable prompt template for code generation.
type PromptTemplate struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	System      string `json:"system" yaml:"system"`
	User        string `json:"user" yaml:"user"`
}

// Format applies the template with the given variables.
func (pt *PromptTemplate) Format(vars map[string]string) (system string, user string) {
	system = pt.System
	user = pt.User
	for k, v := range vars {
		placeholder := "{{" + k + "}}"
		system = strings.ReplaceAll(system, placeholder, v)
		user = strings.ReplaceAll(user, placeholder, v)
	}
	return system, user
}

// CodeGenerator runs inside the builder sandbox and manages code generation
// via Ollama. It sends structured prompts and parses JSON responses.
type CodeGenerator struct {
	builderRT *BuilderRuntime
	kern      *kernel.Kernel
	logger    *zap.Logger
	templates map[string]*PromptTemplate
	maxRounds int
}

// NewCodeGenerator creates a CodeGenerator with loaded prompt templates.
func NewCodeGenerator(br *BuilderRuntime, kern *kernel.Kernel, logger *zap.Logger, templates map[string]*PromptTemplate) (*CodeGenerator, error) {
	if br == nil {
		return nil, fmt.Errorf("builder runtime is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}
	if templates == nil || len(templates) == 0 {
		return nil, fmt.Errorf("at least one prompt template is required")
	}

	return &CodeGenerator{
		builderRT: br,
		kern:      kern,
		logger:    logger,
		templates: templates,
		maxRounds: 3,
	}, nil
}

// Generate sends a code generation request to the builder sandbox via vsock.
// It iterates up to maxRounds, feeding back errors from each round.
func (cg *CodeGenerator) Generate(builderID string, req *CodeGenRequest) (*CodeGenResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid code gen request: %w", err)
	}

	start := time.Now()

	// Apply template if not already set
	if req.SystemPrompt == "" {
		tmpl, ok := cg.templates["skill_codegen"]
		if !ok {
			return nil, fmt.Errorf("skill_codegen template not found")
		}
		specJSON, _ := json.Marshal(req.Spec)
		req.SystemPrompt, _ = tmpl.Format(map[string]string{
			"skill_spec": string(specJSON),
		})
	}

	// Marshal the request
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal codegen request: %w", err)
	}

	msg := kernel.ControlMessage{
		Type:    "codegen.generate",
		Payload: payload,
	}

	// Send to builder sandbox via control plane
	resp, err := cg.builderRT.SendBuildRequest(nil, builderID, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send codegen request to builder %s: %w", builderID, err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("codegen failed in builder %s: %s", builderID, resp.Error)
	}

	// Parse response
	var codeResp CodeGenResponse
	if err := json.Unmarshal(resp.Data, &codeResp); err != nil {
		return nil, fmt.Errorf("failed to parse codegen response: %w", err)
	}

	codeResp.Round = req.Round
	codeResp.Duration = time.Since(start)

	if err := codeResp.Validate(); err != nil {
		return nil, fmt.Errorf("invalid codegen response: %w", err)
	}

	// Audit log the generation
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"builder_id": builderID,
		"skill":      req.Spec.Name,
		"round":      req.Round,
		"files":      len(codeResp.Files),
		"duration":   codeResp.Duration.String(),
	})
	action := kernel.NewAction(kernel.ActionBuilderBuild, "code-generator", auditPayload)
	if _, err := cg.kern.SignAndLog(action); err != nil {
		cg.logger.Error("failed to log code generation", zap.Error(err))
	}

	cg.logger.Info("code generation complete",
		zap.String("builder_id", builderID),
		zap.String("skill", req.Spec.Name),
		zap.Int("round", req.Round),
		zap.Int("files_generated", len(codeResp.Files)),
		zap.Duration("duration", codeResp.Duration),
	)

	return &codeResp, nil
}

// GetTemplate returns a prompt template by name.
func (cg *CodeGenerator) GetTemplate(name string) (*PromptTemplate, bool) {
	t, ok := cg.templates[name]
	return t, ok
}

// DefaultTemplates returns the built-in prompt templates for code generation.
func DefaultTemplates() map[string]*PromptTemplate {
	return map[string]*PromptTemplate{
		"skill_codegen": {
			Name:        "skill_codegen",
			Description: "Generate a complete Go skill implementation",
			System: `You are an expert Go developer building AegisClaw skills.
You write production-quality Go code with full error handling, tests, and documentation.
Security is paramount — validate all inputs, handle all errors, use no unsafe operations.
Output format: JSON object with "files" (map of path to content), "reasoning" (string).`,
			User: `Generate a complete Go skill implementation for the following specification:

{{skill_spec}}

Requirements:
- main.go with vsock-based communication
- Full error handling and structured logging
- Unit tests with >80% coverage
- go.mod with correct module path
- No placeholder or stub code

Return ONLY valid JSON matching this schema:
{
  "files": {"path/file.go": "package content..."},
  "reasoning": "explanation of design decisions"
}`,
		},
		"skill_edit": {
			Name:        "skill_edit",
			Description: "Edit an existing skill based on feedback",
			System: `You are an expert Go developer modifying AegisClaw skills.
You receive existing code and feedback. Apply minimal, targeted changes.
Never remove existing functionality unless explicitly instructed.
Output format: JSON object with "files" (map of path to content), "reasoning" (string).`,
			User: `Modify the following skill based on the feedback provided:

Skill specification:
{{skill_spec}}

Existing code files:
{{existing_code}}

Feedback:
{{feedback}}

Return ONLY valid JSON with updated files and reasoning.`,
		},
		"skill_fix": {
			Name:        "skill_fix",
			Description: "Fix code based on build/test/lint errors",
			System: `You are an expert Go developer fixing build errors in AegisClaw skills.
You receive code with specific errors. Fix ONLY the errors — do not refactor or restructure.
Output format: JSON object with "files" (map of path to content), "reasoning" (string).`,
			User: `Fix the following errors in the skill code:

Skill specification:
{{skill_spec}}

Current code files:
{{existing_code}}

Errors to fix:
{{errors}}

Return ONLY valid JSON with corrected files and reasoning.`,
		},
	}
}
