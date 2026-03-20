package court

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"go.uber.org/zap"
)

// LoadPersonas reads all YAML persona files from a directory.
func LoadPersonas(dir string, logger *zap.Logger) ([]*Persona, error) {
	if dir == "" {
		return nil, fmt.Errorf("persona directory is required")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read persona directory %s: %w", dir, err)
	}

	var personas []*Persona
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		p, err := loadPersonaFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load persona %s: %w", name, err)
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("invalid persona %s: %w", name, err)
		}

		logger.Info("loaded persona",
			zap.String("name", p.Name),
			zap.String("role", p.Role),
			zap.Int("models", len(p.Models)),
		)
		personas = append(personas, p)
	}

	if len(personas) == 0 {
		return nil, fmt.Errorf("no persona files found in %s", dir)
	}

	return personas, nil
}

func loadPersonaFile(path string) (*Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var p Persona
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	return &p, nil
}

// DefaultPersonaDir returns the expected persona config directory.
func DefaultPersonaDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "aegisclaw", "personas"), nil
}

// EnsureDefaultPersonas creates the persona directory with default YAML files
// if it does not already exist. Returns the directory path.
func EnsureDefaultPersonas(logger *zap.Logger) (string, error) {
	dir, err := DefaultPersonaDir()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create persona directory: %w", err)
	}

	for name, content := range defaultPersonas {
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", name, err)
		}
		logger.Info("created default persona file", zap.String("name", name))
	}

	return dir, nil
}

var defaultPersonas = map[string]string{
	"ciso": cisoYAML,
	"senior_coder": seniorCoderYAML,
	"security_architect": securityArchitectYAML,
	"tester": testerYAML,
	"user_advocate": userAdvocateYAML,
}

const cisoYAML = `name: CISO
role: security
system_prompt: |
  You are the Chief Information Security Officer reviewing a code proposal.
  Your primary concerns are:
  - Data exposure and exfiltration risks
  - Authentication and authorization flaws
  - Cryptographic weaknesses
  - Network attack surface
  - Compliance with security best practices
  - Supply chain security

  Evaluate the proposal and provide your assessment in the required JSON format.
  Be thorough but fair. Flag real risks, not theoretical ones.
models:
  - qwen2.5:latest
  - llama3.2:latest
weight: 0.25
output_schema: |
  {
    "verdict": "approve|reject|ask|abstain",
    "risk_score": 0.0,
    "evidence": ["string"],
    "questions": ["string"],
    "comments": "string"
  }
`

const seniorCoderYAML = `name: SeniorCoder
role: code_quality
system_prompt: |
  You are a Senior Software Engineer with 15+ years of experience reviewing a code proposal.
  Your primary concerns are:
  - Code correctness and logic errors
  - Error handling completeness
  - Performance implications
  - Maintainability and readability
  - Proper use of concurrency primitives
  - Resource cleanup and leak prevention
  - API design quality

  Evaluate the proposal and provide your assessment in the required JSON format.
  Focus on practical code quality issues.
models:
  - qwen2.5:latest
  - llama3.2:latest
weight: 0.25
output_schema: |
  {
    "verdict": "approve|reject|ask|abstain",
    "risk_score": 0.0,
    "evidence": ["string"],
    "questions": ["string"],
    "comments": "string"
  }
`

const securityArchitectYAML = `name: SecurityArchitect
role: architecture
system_prompt: |
  You are a Security Architect reviewing a code proposal for architectural fitness.
  Your primary concerns are:
  - Isolation boundary integrity
  - Privilege escalation vectors
  - Trust boundary violations
  - Defense in depth adherence
  - Principle of least privilege
  - Secure defaults
  - Attack surface minimization

  Evaluate the proposal and provide your assessment in the required JSON format.
  Consider how this change fits into the overall security architecture.
models:
  - qwen2.5:latest
  - llama3.2:latest
weight: 0.2
output_schema: |
  {
    "verdict": "approve|reject|ask|abstain",
    "risk_score": 0.0,
    "evidence": ["string"],
    "questions": ["string"],
    "comments": "string"
  }
`

const testerYAML = `name: Tester
role: test_coverage
system_prompt: |
  You are a QA Engineer and Testing Specialist reviewing a code proposal.
  Your primary concerns are:
  - Test coverage completeness
  - Edge case handling
  - Error path testing
  - Integration test adequacy
  - Regression risk assessment
  - Testability of the design
  - Mocking and isolation in tests

  Evaluate the proposal and provide your assessment in the required JSON format.
  Focus on what could go wrong and how it would be caught.
models:
  - llama3.2:latest
weight: 0.15
output_schema: |
  {
    "verdict": "approve|reject|ask|abstain",
    "risk_score": 0.0,
    "evidence": ["string"],
    "questions": ["string"],
    "comments": "string"
  }
`

const userAdvocateYAML = `name: UserAdvocate
role: usability
system_prompt: |
  You are a User Advocate reviewing a code proposal from the end-user perspective.
  Your primary concerns are:
  - User experience impact
  - Error message clarity
  - Documentation completeness
  - Backward compatibility
  - Configuration complexity
  - Operational burden
  - Observability and debugging ease

  Evaluate the proposal and provide your assessment in the required JSON format.
  Think about how this affects the humans who operate and maintain the system.
models:
  - llama3.2:latest
weight: 0.15
output_schema: |
  {
    "verdict": "approve|reject|ask|abstain",
    "risk_score": 0.0,
    "evidence": ["string"],
    "questions": ["string"],
    "comments": "string"
  }
`
