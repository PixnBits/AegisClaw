package securitygate

import (
	"testing"
)

func TestSASTGate_CleanCode(t *testing.T) {
	gate := NewSASTGate()
	req := &EvalRequest{
		ProposalID: "test-001",
		SkillName:  "hello",
		Files: map[string]string{
			"main.go": `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean code to pass SAST")
		for _, f := range result.Findings {
			t.Logf("  finding: %s %s %s", f.Rule, f.Severity, f.Message)
		}
	}
}

func TestSASTGate_DetectsWeakCrypto(t *testing.T) {
	gate := NewSASTGate()
	req := &EvalRequest{
		ProposalID: "test-002",
		SkillName:  "crypto-test",
		Files: map[string]string{
			"hash.go": `package main

import "crypto/md5"

func badHash(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected weak crypto to fail SAST")
	}

	found := false
	for _, f := range result.Findings {
		if f.Rule == "G401" {
			found = true
		}
	}
	if !found {
		t.Error("expected G401 finding for weak crypto")
	}
}

func TestSASTGate_DetectsHardcodedSecret(t *testing.T) {
	gate := NewSASTGate()
	req := &EvalRequest{
		ProposalID: "test-003",
		SkillName:  "secret-test",
		Files: map[string]string{
			"config.go": `package main

var password = "super_secret_password_123"
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected hardcoded secret to fail SAST")
	}

	found := false
	for _, f := range result.Findings {
		if f.Rule == "G101" {
			found = true
		}
	}
	if !found {
		t.Error("expected G101 finding for hardcoded secret")
	}
}

func TestSASTGate_SkipsNonGoFiles(t *testing.T) {
	gate := NewSASTGate()
	req := &EvalRequest{
		ProposalID: "test-004",
		SkillName:  "readme-test",
		Files: map[string]string{
			"README.md": `# My Skill

password = "not_really_code"
crypto/md5 reference in docs
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("expected non-Go files to be skipped")
	}
}

func TestSCAGate_CleanGoMod(t *testing.T) {
	gate := NewSCAGate()
	req := &EvalRequest{
		ProposalID: "test-010",
		SkillName:  "clean-deps",
		Files: map[string]string{
			"go.mod": `module example.com/skill

go 1.21

require (
	github.com/google/uuid v1.6.0
)
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean go.mod to pass SCA")
	}
}

func TestSCAGate_DetectsBannedDep(t *testing.T) {
	gate := NewSCAGate()
	req := &EvalRequest{
		ProposalID: "test-011",
		SkillName:  "bad-deps",
		Files: map[string]string{
			"go.mod": `module example.com/skill

go 1.21

require (
	github.com/dgrijalva/jwt-go v3.2.0
)
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected banned dependency to fail SCA")
	}

	found := false
	for _, f := range result.Findings {
		if f.Rule == "SCA-BANNED" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCA-BANNED finding")
	}
}

func TestSCAGate_DetectsUnpinnedNpmDep(t *testing.T) {
	gate := NewSCAGate()
	req := &EvalRequest{
		ProposalID: "test-012",
		SkillName:  "npm-test",
		Files: map[string]string{
			"package.json": `{
  "name": "test",
  "dependencies": {
    "lodash": "*",
    "express": "^4.18.0"
  }
}`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected unpinned npm dependency to fail SCA")
	}

	found := false
	for _, f := range result.Findings {
		if f.Rule == "SCA-UNPINNED" {
			found = true
		}
	}
	if !found {
		t.Error("expected SCA-UNPINNED finding")
	}
}

func TestSecretsGate_CleanCode(t *testing.T) {
	gate := NewSecretsGate()
	req := &EvalRequest{
		ProposalID: "test-020",
		SkillName:  "clean",
		Files: map[string]string{
			"main.go": `package main

func main() {}
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean code to pass secrets scanning")
	}
}

func TestSecretsGate_DetectsAWSKey(t *testing.T) {
	gate := NewSecretsGate()
	req := &EvalRequest{
		ProposalID: "test-021",
		SkillName:  "leaked",
		Files: map[string]string{
			"config.go": `package main

const awsKey = "AKIAIOSFODNN7EXAMPLE"
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected AWS key to fail secrets scanning")
	}
}

func TestSecretsGate_DetectsPrivateKey(t *testing.T) {
	gate := NewSecretsGate()
	req := &EvalRequest{
		ProposalID: "test-022",
		SkillName:  "key-leak",
		Files: map[string]string{
			"key.pem": `-----BEGIN RSA PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQ...
-----END RSA PRIVATE KEY-----`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected private key to fail secrets scanning")
	}
}

func TestPolicyGate_CleanCode(t *testing.T) {
	gate := NewPolicyGate(DefaultPolicies())
	req := &EvalRequest{
		ProposalID: "test-030",
		SkillName:  "clean",
		Files: map[string]string{
			"main.go": `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean code to pass all policies")
		for _, f := range result.Findings {
			t.Logf("  finding: %s %s %s", f.Rule, f.Severity, f.Message)
		}
	}
}

func TestPolicyGate_DetectsPrivilegedOps(t *testing.T) {
	gate := NewPolicyGate(DefaultPolicies())
	req := &EvalRequest{
		ProposalID: "test-031",
		SkillName:  "privileged",
		Files: map[string]string{
			"main.go": `package main

import "syscall"

func escalate() {
	syscall.Setuid(0)
}
`,
		},
	}

	result, err := gate.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected privileged ops to fail policy gate")
	}

	found := false
	for _, f := range result.Findings {
		if f.Rule == "POL-NO-PRIVILEGED-OPS" {
			found = true
		}
	}
	if !found {
		t.Error("expected POL-NO-PRIVILEGED-OPS finding")
	}
}

func TestPipelineDefault(t *testing.T) {
	pipeline := DefaultPipeline(DefaultPolicies())
	req := &EvalRequest{
		ProposalID: "test-040",
		SkillName:  "safe-skill",
		Files: map[string]string{
			"main.go": `package main

import "fmt"

func main() {
	fmt.Println("Hello from a safe skill!")
}
`,
		},
	}

	result, err := pipeline.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if !result.Passed {
		t.Error("expected safe code to pass all gates")
		for _, g := range result.Gates {
			for _, f := range g.Findings {
				t.Logf("  [%s] %s: %s %s", g.Gate, f.Rule, f.Severity, f.Message)
			}
		}
	}
	if len(result.Gates) != 4 {
		t.Errorf("expected 4 gates, got %d", len(result.Gates))
	}
}

func TestPipelineBlocksUnsafe(t *testing.T) {
	pipeline := DefaultPipeline(DefaultPolicies())
	req := &EvalRequest{
		ProposalID: "test-041",
		SkillName:  "unsafe-skill",
		Files: map[string]string{
			"main.go": `package main

import "crypto/md5"

var password = "hardcoded_secret_value"

func badHash(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}
`,
		},
	}

	result, err := pipeline.Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result.Passed {
		t.Error("expected unsafe code to fail pipeline")
	}
	if result.BlockingFindings == 0 {
		t.Error("expected blocking findings")
	}
}

func TestPipelineValidation(t *testing.T) {
	pipeline := DefaultPipeline(DefaultPolicies())

	// Missing required fields.
	_, err := pipeline.Evaluate(&EvalRequest{})
	if err == nil {
		t.Error("expected error for empty request")
	}
}

func TestGateResultHasBlocking(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     bool
	}{
		{
			name: "no findings",
			want: false,
		},
		{
			name: "warning only",
			findings: []Finding{
				{Severity: SeverityWarning},
			},
			want: false,
		},
		{
			name: "error finding",
			findings: []Finding{
				{Severity: SeverityError},
			},
			want: true,
		},
		{
			name: "critical finding",
			findings: []Finding{
				{Severity: SeverityCritical},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gr := &GateResult{Findings: tt.findings}
			if got := gr.HasBlocking(); got != tt.want {
				t.Errorf("HasBlocking() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     EvalRequest
		wantErr bool
	}{
		{
			name: "valid",
			req: EvalRequest{
				ProposalID: "test",
				SkillName:  "skill",
				Files:      map[string]string{"a.go": "code"},
			},
		},
		{
			name:    "missing proposal id",
			req:     EvalRequest{SkillName: "s", Files: map[string]string{"a": "b"}},
			wantErr: true,
		},
		{
			name:    "missing skill name",
			req:     EvalRequest{ProposalID: "p", Files: map[string]string{"a": "b"}},
			wantErr: true,
		},
		{
			name:    "missing files",
			req:     EvalRequest{ProposalID: "p", SkillName: "s"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
