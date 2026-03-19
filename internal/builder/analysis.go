package builder

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// AnalysisSeverity classifies the severity of an analysis finding.
type AnalysisSeverity string

const (
	SeverityInfo     AnalysisSeverity = "info"
	SeverityWarning  AnalysisSeverity = "warning"
	SeverityError    AnalysisSeverity = "error"
	SeverityCritical AnalysisSeverity = "critical"
)

// AnalysisFinding represents a single finding from static analysis or build.
type AnalysisFinding struct {
	Tool     string           `json:"tool"`
	Severity AnalysisSeverity `json:"severity"`
	File     string           `json:"file"`
	Line     int              `json:"line"`
	Column   int              `json:"column"`
	Message  string           `json:"message"`
	Rule     string           `json:"rule,omitempty"`
}

// AnalysisResult captures the output of all analysis steps.
type AnalysisResult struct {
	ProposalID    string            `json:"proposal_id"`
	Diff          string            `json:"diff"`
	TestOutput    string            `json:"test_output"`
	TestPassed    bool              `json:"test_passed"`
	LintOutput    string            `json:"lint_output"`
	LintPassed    bool              `json:"lint_passed"`
	SecurityOutput string           `json:"security_output"`
	SecurityPassed bool             `json:"security_passed"`
	BuildOutput   string            `json:"build_output"`
	BuildPassed   bool              `json:"build_passed"`
	Findings      []AnalysisFinding `json:"findings"`
	BinaryHash    string            `json:"binary_hash,omitempty"`
	Passed        bool              `json:"passed"`
	FailureReason string            `json:"failure_reason,omitempty"`
	Duration      time.Duration     `json:"duration"`
	CompletedAt   time.Time         `json:"completed_at"`
}

// HasHighSeverity returns true if any finding is error or critical.
func (ar *AnalysisResult) HasHighSeverity() bool {
	for _, f := range ar.Findings {
		if f.Severity == SeverityError || f.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// SummaryByTool returns finding counts grouped by tool.
func (ar *AnalysisResult) SummaryByTool() map[string]int {
	summary := make(map[string]int)
	for _, f := range ar.Findings {
		summary[f.Tool]++
	}
	return summary
}

// AnalysisRequest is sent to the builder sandbox to run analysis.
type AnalysisRequest struct {
	ProposalID string            `json:"proposal_id"`
	Files      map[string]string `json:"files"`
	Diff       string            `json:"diff"`
	SkillName  string            `json:"skill_name"`
}

// Validate checks the analysis request.
func (ar *AnalysisRequest) Validate() error {
	if ar.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if len(ar.Files) == 0 {
		return fmt.Errorf("files are required for analysis")
	}
	if ar.SkillName == "" {
		return fmt.Errorf("skill name is required")
	}
	return nil
}

// Analyzer runs static analysis, tests, and builds inside the builder sandbox.
type Analyzer struct {
	builderRT *BuilderRuntime
	kern      *kernel.Kernel
	logger    *zap.Logger
}

// NewAnalyzer creates an Analyzer for the builder sandbox.
func NewAnalyzer(br *BuilderRuntime, kern *kernel.Kernel, logger *zap.Logger) (*Analyzer, error) {
	if br == nil {
		return nil, fmt.Errorf("builder runtime is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}

	return &Analyzer{
		builderRT: br,
		kern:      kern,
		logger:    logger,
	}, nil
}

// Analyze sends the code to the builder sandbox for full analysis:
// go test → golangci-lint → gosec → go build -buildmode=pie
func (a *Analyzer) Analyze(builderID string, req *AnalysisRequest) (*AnalysisResult, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid analysis request: %w", err)
	}

	start := time.Now()

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal analysis request: %w", err)
	}

	msg := kernel.ControlMessage{
		Type:    "analysis.run",
		Payload: payload,
	}

	resp, err := a.builderRT.SendBuildRequest(nil, builderID, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send analysis request to builder %s: %w", builderID, err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("analysis failed in builder %s: %s", builderID, resp.Error)
	}

	var result AnalysisResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse analysis response: %w", err)
	}

	result.ProposalID = req.ProposalID
	result.Diff = req.Diff
	result.Duration = time.Since(start)
	result.CompletedAt = time.Now().UTC()

	// Determine overall pass/fail
	result.Passed = result.TestPassed && result.LintPassed && result.SecurityPassed && result.BuildPassed

	// Fail on high severity findings
	if result.HasHighSeverity() {
		result.Passed = false
		result.FailureReason = "high severity findings detected"
	}

	// Audit log
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"proposal_id":     req.ProposalID,
		"builder_id":      builderID,
		"test_passed":     result.TestPassed,
		"lint_passed":     result.LintPassed,
		"security_passed": result.SecurityPassed,
		"build_passed":    result.BuildPassed,
		"findings":        len(result.Findings),
		"passed":          result.Passed,
		"duration":        result.Duration.String(),
	})
	action := kernel.NewAction(kernel.ActionBuilderBuild, "analyzer", auditPayload)
	if _, logErr := a.kern.SignAndLog(action); logErr != nil {
		a.logger.Error("failed to log analysis result", zap.Error(logErr))
	}

	a.logger.Info("analysis complete",
		zap.String("proposal_id", req.ProposalID),
		zap.Bool("passed", result.Passed),
		zap.Bool("tests", result.TestPassed),
		zap.Bool("lint", result.LintPassed),
		zap.Bool("security", result.SecurityPassed),
		zap.Bool("build", result.BuildPassed),
		zap.Int("findings", len(result.Findings)),
		zap.Duration("duration", result.Duration),
	)

	return &result, nil
}

// ParseGolangCIOutput parses golangci-lint JSON output into findings.
func ParseGolangCIOutput(output string) []AnalysisFinding {
	var findings []AnalysisFinding

	var lintResult struct {
		Issues []struct {
			FromLinter string `json:"FromLinter"`
			Text       string `json:"Text"`
			Pos        struct {
				Filename string `json:"Filename"`
				Line     int    `json:"Line"`
				Column   int    `json:"Column"`
			} `json:"Pos"`
			Severity string `json:"Severity"`
		} `json:"Issues"`
	}

	if err := json.Unmarshal([]byte(output), &lintResult); err != nil {
		// If not JSON, treat as raw text
		if strings.TrimSpace(output) != "" {
			findings = append(findings, AnalysisFinding{
				Tool:     "golangci-lint",
				Severity: SeverityWarning,
				Message:  strings.TrimSpace(output),
			})
		}
		return findings
	}

	for _, issue := range lintResult.Issues {
		severity := SeverityWarning
		switch issue.Severity {
		case "error":
			severity = SeverityError
		case "warning":
			severity = SeverityWarning
		}

		findings = append(findings, AnalysisFinding{
			Tool:     "golangci-lint:" + issue.FromLinter,
			Severity: severity,
			File:     issue.Pos.Filename,
			Line:     issue.Pos.Line,
			Column:   issue.Pos.Column,
			Message:  issue.Text,
			Rule:     issue.FromLinter,
		})
	}

	return findings
}

// ParseGosecOutput parses gosec JSON output into findings.
func ParseGosecOutput(output string) []AnalysisFinding {
	var findings []AnalysisFinding

	var gosecResult struct {
		Issues []struct {
			Severity   string `json:"severity"`
			Confidence string `json:"confidence"`
			RuleID     string `json:"rule_id"`
			Details    string `json:"details"`
			File       string `json:"file"`
			Line       string `json:"line"`
			Column     string `json:"column"`
		} `json:"Issues"`
	}

	if err := json.Unmarshal([]byte(output), &gosecResult); err != nil {
		if strings.TrimSpace(output) != "" {
			findings = append(findings, AnalysisFinding{
				Tool:     "gosec",
				Severity: SeverityWarning,
				Message:  strings.TrimSpace(output),
			})
		}
		return findings
	}

	for _, issue := range gosecResult.Issues {
		severity := SeverityWarning
		switch strings.ToUpper(issue.Severity) {
		case "HIGH":
			severity = SeverityCritical
		case "MEDIUM":
			severity = SeverityError
		case "LOW":
			severity = SeverityWarning
		}

		findings = append(findings, AnalysisFinding{
			Tool:     "gosec",
			Severity: severity,
			File:     issue.File,
			Message:  issue.Details,
			Rule:     issue.RuleID,
		})
	}

	return findings
}

// ParseTestOutput parses go test output for failure information.
func ParseTestOutput(output string, passed bool) []AnalysisFinding {
	var findings []AnalysisFinding
	if passed {
		return findings
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "FAIL") {
			findings = append(findings, AnalysisFinding{
				Tool:     "go-test",
				Severity: SeverityError,
				Message:  trimmed,
			})
		}
	}

	if len(findings) == 0 && !passed {
		findings = append(findings, AnalysisFinding{
			Tool:     "go-test",
			Severity: SeverityError,
			Message:  "tests failed (see test output for details)",
		})
	}

	return findings
}

// ComputeBinaryHash returns the SHA-256 hex hash of binary data.
func ComputeBinaryHash(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
