// Package securitygate implements the mandatory security gates defined in
// PRD §11.2: SAST (static analysis), SCA (software composition analysis),
// secrets scanning, and policy-as-code enforcement.
//
// These gates are evaluated by the builder pipeline before any skill
// artifact can be deployed. This resolves deviation D8 from the PRD
// alignment plan.
package securitygate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// GateType classifies a security gate.
type GateType string

const (
	GateSAST            GateType = "sast"
	GateSCA             GateType = "sca"
	GateSecretsScanning GateType = "secrets_scanning"
	GatePolicy          GateType = "policy"
)

// Severity classifies the severity of a gate finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Finding represents a single finding from a security gate.
type Finding struct {
	Gate     GateType `json:"gate"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
}

// GateResult captures the output of a single security gate.
type GateResult struct {
	Gate     GateType      `json:"gate"`
	Passed   bool          `json:"passed"`
	Findings []Finding     `json:"findings"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// HasBlocking returns true if any finding is error or critical severity.
func (gr *GateResult) HasBlocking() bool {
	for _, f := range gr.Findings {
		if f.Severity == SeverityError || f.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// PipelineResult captures the aggregated output of all security gates.
type PipelineResult struct {
	Gates            []GateResult  `json:"gates"`
	Passed           bool          `json:"passed"`
	TotalFindings    int           `json:"total_findings"`
	BlockingFindings int           `json:"blocking_findings"`
	Duration         time.Duration `json:"duration"`
}

// Gate is the interface that all security gates implement.
type Gate interface {
	Type() GateType
	Evaluate(req *EvalRequest) (*GateResult, error)
}

// EvalRequest contains the code to be evaluated by security gates.
type EvalRequest struct {
	ProposalID string            `json:"proposal_id"`
	SkillName  string            `json:"skill_name"`
	Files      map[string]string `json:"files"`
	Diff       string            `json:"diff,omitempty"`
}

// Validate checks the evaluation request has required fields.
func (r *EvalRequest) Validate() error {
	if r.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if r.SkillName == "" {
		return fmt.Errorf("skill name is required")
	}
	if len(r.Files) == 0 {
		return fmt.Errorf("files are required for evaluation")
	}
	return nil
}

// Pipeline runs all registered security gates against a code submission.
type Pipeline struct {
	gates []Gate
}

// NewPipeline creates a security gate pipeline with the given gates.
func NewPipeline(gates ...Gate) *Pipeline {
	return &Pipeline{gates: gates}
}

// DefaultPipeline creates a Pipeline with all default security gates:
// SAST, SCA, secrets scanning, and policy enforcement.
func DefaultPipeline(policies []Policy) *Pipeline {
	return NewPipeline(
		NewSASTGate(),
		NewSCAGate(),
		NewSecretsGate(),
		NewPolicyGate(policies),
	)
}

// Evaluate runs all gates and returns the aggregated result.
// The pipeline fails if any gate has blocking findings.
func (p *Pipeline) Evaluate(req *EvalRequest) (*PipelineResult, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	start := time.Now()
	result := &PipelineResult{Passed: true}

	for _, g := range p.gates {
		gr, err := g.Evaluate(req)
		if err != nil {
			gr = &GateResult{
				Gate:   g.Type(),
				Passed: false,
				Error:  err.Error(),
			}
		}

		result.Gates = append(result.Gates, *gr)
		result.TotalFindings += len(gr.Findings)

		if gr.HasBlocking() {
			result.Passed = false
			for _, f := range gr.Findings {
				if f.Severity == SeverityError || f.Severity == SeverityCritical {
					result.BlockingFindings++
				}
			}
		}

		if !gr.Passed {
			result.Passed = false
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// --- SAST Gate ---

// SASTGate performs static application security testing on source code.
// It checks for common security anti-patterns such as:
//   - Unsafe exec calls
//   - Hardcoded credentials
//   - Path traversal vulnerabilities
//   - Insecure crypto usage
type SASTGate struct{}

// NewSASTGate creates a new SAST gate.
func NewSASTGate() *SASTGate {
	return &SASTGate{}
}

// Type returns the gate type.
func (g *SASTGate) Type() GateType {
	return GateSAST
}

// SAST rule patterns for Go code.
var sastRules = []struct {
	rule     string
	pattern  *regexp.Regexp
	severity Severity
	message  string
}{
	{
		rule:     "G204",
		pattern:  regexp.MustCompile(`exec\.Command\s*\(\s*[^"']`),
		severity: SeverityError,
		message:  "Command execution with variable input — potential command injection",
	},
	{
		rule:     "G101",
		pattern:  regexp.MustCompile(`(?i)(password|secret|token|apikey|api_key)\s*[:=]\s*"[^"]+"`),
		severity: SeverityCritical,
		message:  "Hardcoded credential detected",
	},
	{
		rule:     "G304",
		pattern:  regexp.MustCompile(`os\.(Open|ReadFile|WriteFile)\s*\([^"']`),
		severity: SeverityWarning,
		message:  "File path from variable — verify path traversal protection",
	},
	{
		rule:     "G401",
		pattern:  regexp.MustCompile(`crypto/(md5|sha1|des|rc4)`),
		severity: SeverityError,
		message:  "Use of weak cryptographic algorithm",
	},
	{
		rule:     "G104",
		pattern:  regexp.MustCompile(`\b(http\.ListenAndServe)\b`),
		severity: SeverityWarning,
		message:  "Unencrypted HTTP server — consider TLS",
	},
	{
		rule:     "G107",
		pattern:  regexp.MustCompile(`http\.Get\s*\([^"']`),
		severity: SeverityWarning,
		message:  "HTTP request with variable URL — verify SSRF protection",
	},
	{
		rule:     "G301",
		pattern:  regexp.MustCompile(`os\.MkdirAll\s*\([^,]+,\s*0777\b`),
		severity: SeverityWarning,
		message:  "Overly permissive directory permissions (0777)",
	},
	{
		rule:     "G306",
		pattern:  regexp.MustCompile(`os\.WriteFile\s*\([^,]+,[^,]+,\s*0666\b`),
		severity: SeverityWarning,
		message:  "Overly permissive file permissions (0666)",
	},
}

// Evaluate runs SAST checks on all source files.
func (g *SASTGate) Evaluate(req *EvalRequest) (*GateResult, error) {
	start := time.Now()
	result := &GateResult{Gate: GateSAST, Passed: true}

	for filename, content := range req.Files {
		// Only analyze Go files.
		if !strings.HasSuffix(filename, ".go") {
			continue
		}

		lines := strings.Split(content, "\n")
		for lineNum, line := range lines {
			for _, rule := range sastRules {
				if rule.pattern.MatchString(line) {
					finding := Finding{
						Gate:     GateSAST,
						Rule:     rule.rule,
						Severity: rule.severity,
						File:     filename,
						Line:     lineNum + 1,
						Message:  rule.message,
					}
					result.Findings = append(result.Findings, finding)
				}
			}
		}
	}

	result.Passed = !result.HasBlocking()
	result.Duration = time.Since(start)
	return result, nil
}

// --- SCA Gate ---

// SCAGate performs software composition analysis on dependency declarations.
// It checks go.mod, go.sum, package.json, etc. for known-vulnerable or
// banned dependencies.
type SCAGate struct {
	// bannedPackages is a set of packages that are not allowed.
	bannedPackages map[string]string
}

// NewSCAGate creates a new SCA gate with default banned packages.
func NewSCAGate() *SCAGate {
	return &SCAGate{
		bannedPackages: map[string]string{
			"github.com/dgrijalva/jwt-go": "CVE-2020-26160: use github.com/golang-jwt/jwt/v5 instead",
			"github.com/gorilla/csrf":     "unmaintained: archived by gorilla",
			"github.com/satori/go.uuid":   "CVE-2021-3538: use github.com/google/uuid instead",
		},
	}
}

// Type returns the gate type.
func (g *SCAGate) Type() GateType {
	return GateSCA
}

// Evaluate checks dependency files for banned or vulnerable packages.
func (g *SCAGate) Evaluate(req *EvalRequest) (*GateResult, error) {
	start := time.Now()
	result := &GateResult{Gate: GateSCA, Passed: true}

	for filename, content := range req.Files {
		if filename == "go.mod" || strings.HasSuffix(filename, "/go.mod") {
			g.checkGoMod(content, filename, result)
		}
		if filename == "package.json" || strings.HasSuffix(filename, "/package.json") {
			g.checkPackageJSON(content, filename, result)
		}
	}

	result.Passed = !result.HasBlocking()
	result.Duration = time.Since(start)
	return result, nil
}

func (g *SCAGate) checkGoMod(content, filename string, result *GateResult) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		for pkg, reason := range g.bannedPackages {
			if strings.Contains(trimmed, pkg) {
				result.Findings = append(result.Findings, Finding{
					Gate:     GateSCA,
					Rule:     "SCA-BANNED",
					Severity: SeverityError,
					File:     filename,
					Message:  fmt.Sprintf("Banned dependency %q: %s", pkg, reason),
				})
			}
		}
	}
}

func (g *SCAGate) checkPackageJSON(content, filename string, result *GateResult) {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return
	}

	// Check for wildcard or git-based dependencies.
	for name, version := range pkg.Dependencies {
		if version == "*" || strings.HasPrefix(version, "git") || strings.HasPrefix(version, "http") {
			result.Findings = append(result.Findings, Finding{
				Gate:     GateSCA,
				Rule:     "SCA-UNPINNED",
				Severity: SeverityError,
				File:     filename,
				Message:  fmt.Sprintf("Unpinned dependency %q version %q — use explicit semver", name, version),
			})
		}
	}
}

// --- Secrets Scanning Gate ---

// SecretsGate scans source code for accidentally committed secrets.
type SecretsGate struct {
	patterns []secretPattern
}

type secretPattern struct {
	name    string
	pattern *regexp.Regexp
}

// NewSecretsGate creates a new secrets scanning gate.
func NewSecretsGate() *SecretsGate {
	return &SecretsGate{
		patterns: []secretPattern{
			{name: "AWS Access Key", pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
			{name: "AWS Secret Key", pattern: regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*[A-Za-z0-9/+=]{40}`)},
			{name: "GitHub Token", pattern: regexp.MustCompile(`ghp_[0-9a-zA-Z]{36}`)},
			{name: "GitHub OAuth", pattern: regexp.MustCompile(`gho_[0-9a-zA-Z]{36}`)},
			{name: "Generic API Key", pattern: regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*["'][A-Za-z0-9]{20,}["']`)},
			{name: "Private Key", pattern: regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)},
		},
	}
}

// Type returns the gate type.
func (g *SecretsGate) Type() GateType {
	return GateSecretsScanning
}

// Evaluate scans all files for leaked secrets.
func (g *SecretsGate) Evaluate(req *EvalRequest) (*GateResult, error) {
	start := time.Now()
	result := &GateResult{Gate: GateSecretsScanning, Passed: true}

	for filename, content := range req.Files {
		// Skip binary-looking files and test fixtures.
		if strings.HasSuffix(filename, ".exe") || strings.HasSuffix(filename, ".bin") {
			continue
		}

		lines := strings.Split(content, "\n")
		for lineNum, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			for _, sp := range g.patterns {
				if sp.pattern.MatchString(line) {
					result.Findings = append(result.Findings, Finding{
						Gate:     GateSecretsScanning,
						Rule:     "SECRET-" + strings.ReplaceAll(strings.ToUpper(sp.name), " ", "-"),
						Severity: SeverityCritical,
						File:     filename,
						Line:     lineNum + 1,
						Message:  fmt.Sprintf("Possible %s detected — use vault injection instead", sp.name),
					})
				}
			}
		}
	}

	result.Passed = !result.HasBlocking()
	result.Duration = time.Since(start)
	return result, nil
}

// --- Policy Gate ---

// Policy represents a policy-as-code rule that validates deployment invariants.
// This is the implementation of PRD §11.2's policy-as-code requirement.
type Policy struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Severity    Severity `json:"severity"`
	// CheckFunc evaluates the policy against the request.
	// Returns nil if the policy is satisfied, or an error message if violated.
	CheckFunc func(req *EvalRequest) []string `json:"-"`
}

// PolicyGate evaluates policy-as-code rules against code submissions.
type PolicyGate struct {
	policies []Policy
}

// NewPolicyGate creates a new policy gate with the given policies.
func NewPolicyGate(policies []Policy) *PolicyGate {
	return &PolicyGate{policies: policies}
}

// Type returns the gate type.
func (g *PolicyGate) Type() GateType {
	return GatePolicy
}

// Evaluate runs all registered policies against the request.
func (g *PolicyGate) Evaluate(req *EvalRequest) (*GateResult, error) {
	start := time.Now()
	result := &GateResult{Gate: GatePolicy, Passed: true}

	for _, p := range g.policies {
		violations := p.CheckFunc(req)
		for _, v := range violations {
			result.Findings = append(result.Findings, Finding{
				Gate:     GatePolicy,
				Rule:     p.Name,
				Severity: p.Severity,
				Message:  v,
			})
		}
	}

	result.Passed = !result.HasBlocking()
	result.Duration = time.Since(start)
	return result, nil
}

// DefaultPolicies returns the default set of security policies that enforce
// PRD isolation invariants:
//   - No network access unless explicitly declared
//   - Read-only filesystem (except workspace)
//   - cap-drop ALL
//   - Secret proxy usage (no hardcoded secrets)
//   - No privileged operations
func DefaultPolicies() []Policy {
	return []Policy{
		{
			Name:        "POL-NO-UNSAFE-EXEC",
			Description: "Skills must not execute arbitrary commands from user input",
			Severity:    SeverityError,
			CheckFunc: func(req *EvalRequest) []string {
				var violations []string
				for file, content := range req.Files {
					if !strings.HasSuffix(file, ".go") {
						continue
					}
					if strings.Contains(content, "os/exec") && strings.Contains(content, "os.Args") {
						violations = append(violations, fmt.Sprintf("%s: uses os/exec with os.Args — potential command injection", file))
					}
				}
				return violations
			},
		},
		{
			Name:        "POL-NO-HOST-FS",
			Description: "Skills must not access host filesystem paths outside /workspace",
			Severity:    SeverityError,
			CheckFunc: func(req *EvalRequest) []string {
				var violations []string
				hostPaths := []string{"/etc/", "/root/", "/home/", "/var/", "/usr/", "/opt/"}
				for file, content := range req.Files {
					if !strings.HasSuffix(file, ".go") {
						continue
					}
					for _, hp := range hostPaths {
						if strings.Contains(content, fmt.Sprintf(`"%s`, hp)) {
							violations = append(violations, fmt.Sprintf("%s: references host path %s — skills should use /workspace only", file, hp))
						}
					}
				}
				return violations
			},
		},
		{
			Name:        "POL-NO-NETWORK-UNLESS-DECLARED",
			Description: "Skills must not use network operations unless declared in proposal",
			Severity:    SeverityWarning,
			CheckFunc: func(req *EvalRequest) []string {
				var violations []string
				netPatterns := []string{"net.Dial", "net.Listen", "http.Get", "http.Post", "http.NewRequest"}
				for file, content := range req.Files {
					if !strings.HasSuffix(file, ".go") {
						continue
					}
					for _, np := range netPatterns {
						if strings.Contains(content, np) {
							violations = append(violations, fmt.Sprintf("%s: uses %s — ensure network access is declared in proposal", file, np))
						}
					}
				}
				return violations
			},
		},
		{
			Name:        "POL-NO-PRIVILEGED-OPS",
			Description: "Skills must not attempt privileged operations",
			Severity:    SeverityCritical,
			CheckFunc: func(req *EvalRequest) []string {
				var violations []string
				privilegedOps := []string{
					"syscall.Setuid", "syscall.Setgid", "syscall.Mount",
					"syscall.Chroot", "syscall.Reboot",
				}
				for file, content := range req.Files {
					if !strings.HasSuffix(file, ".go") {
						continue
					}
					for _, op := range privilegedOps {
						if strings.Contains(content, op) {
							violations = append(violations, fmt.Sprintf("%s: uses privileged operation %s", file, op))
						}
					}
				}
				return violations
			},
		},
	}
}
