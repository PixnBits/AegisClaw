#!/usr/bin/env python3
"""Writes internal/builder/analysis_test.go — Tests for analysis, parsing, and severity logic."""
import os

code = r'''package builder

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAnalysisResultHasHighSeverity(t *testing.T) {
	tests := []struct {
		name     string
		findings []AnalysisFinding
		want     bool
	}{
		{
			"no findings",
			nil,
			false,
		},
		{
			"info only",
			[]AnalysisFinding{{Severity: SeverityInfo}},
			false,
		},
		{
			"warning only",
			[]AnalysisFinding{{Severity: SeverityWarning}},
			false,
		},
		{
			"has error",
			[]AnalysisFinding{{Severity: SeverityWarning}, {Severity: SeverityError}},
			true,
		},
		{
			"has critical",
			[]AnalysisFinding{{Severity: SeverityInfo}, {Severity: SeverityCritical}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := &AnalysisResult{Findings: tt.findings}
			if got := ar.HasHighSeverity(); got != tt.want {
				t.Errorf("HasHighSeverity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysisResultSummaryByTool(t *testing.T) {
	ar := &AnalysisResult{
		Findings: []AnalysisFinding{
			{Tool: "golangci-lint:govet", Severity: SeverityWarning},
			{Tool: "golangci-lint:govet", Severity: SeverityError},
			{Tool: "gosec", Severity: SeverityCritical},
			{Tool: "go-test", Severity: SeverityError},
		},
	}

	summary := ar.SummaryByTool()
	if summary["golangci-lint:govet"] != 2 {
		t.Errorf("expected 2 govet findings, got %d", summary["golangci-lint:govet"])
	}
	if summary["gosec"] != 1 {
		t.Errorf("expected 1 gosec finding, got %d", summary["gosec"])
	}
	if summary["go-test"] != 1 {
		t.Errorf("expected 1 go-test finding, got %d", summary["go-test"])
	}
}

func TestAnalysisRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *AnalysisRequest
		wantErr string
	}{
		{
			"empty proposal ID",
			&AnalysisRequest{ProposalID: "", Files: map[string]string{"a.go": "x"}, SkillName: "test"},
			"proposal ID is required",
		},
		{
			"empty files",
			&AnalysisRequest{ProposalID: "p-1", Files: nil, SkillName: "test"},
			"files are required",
		},
		{
			"empty skill name",
			&AnalysisRequest{ProposalID: "p-1", Files: map[string]string{"a.go": "x"}, SkillName: ""},
			"skill name is required",
		},
		{
			"valid request",
			&AnalysisRequest{
				ProposalID: "p-1",
				Files:      map[string]string{"main.go": "package main\n"},
				SkillName:  "test-skill",
			},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestNewAnalyzerValidation(t *testing.T) {
	_, err := NewAnalyzer(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil builder runtime")
	}
	if !containsStr(err.Error(), "builder runtime is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseGolangCIOutputJSON(t *testing.T) {
	lintJSON := `{
		"Issues": [
			{
				"FromLinter": "govet",
				"Text": "unreachable code",
				"Pos": {"Filename": "main.go", "Line": 10, "Column": 2},
				"Severity": "error"
			},
			{
				"FromLinter": "errcheck",
				"Text": "error return value not checked",
				"Pos": {"Filename": "handler.go", "Line": 25, "Column": 5},
				"Severity": "warning"
			}
		]
	}`

	findings := ParseGolangCIOutput(lintJSON)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	if findings[0].Tool != "golangci-lint:govet" {
		t.Errorf("expected tool golangci-lint:govet, got %s", findings[0].Tool)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected error severity, got %s", findings[0].Severity)
	}
	if findings[0].File != "main.go" {
		t.Errorf("expected file main.go, got %s", findings[0].File)
	}
	if findings[0].Line != 10 {
		t.Errorf("expected line 10, got %d", findings[0].Line)
	}

	if findings[1].Tool != "golangci-lint:errcheck" {
		t.Errorf("expected tool golangci-lint:errcheck, got %s", findings[1].Tool)
	}
	if findings[1].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %s", findings[1].Severity)
	}
	if findings[1].Rule != "errcheck" {
		t.Errorf("expected rule errcheck, got %s", findings[1].Rule)
	}
}

func TestParseGolangCIOutputRawText(t *testing.T) {
	findings := ParseGolangCIOutput("some raw lint output")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding from raw text, got %d", len(findings))
	}
	if findings[0].Tool != "golangci-lint" {
		t.Errorf("expected tool golangci-lint, got %s", findings[0].Tool)
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %s", findings[0].Severity)
	}
}

func TestParseGolangCIOutputEmpty(t *testing.T) {
	findings := ParseGolangCIOutput("")
	if len(findings) != 0 {
		t.Errorf("expected 0 findings from empty output, got %d", len(findings))
	}
	findings = ParseGolangCIOutput("   ")
	if len(findings) != 0 {
		t.Errorf("expected 0 findings from whitespace output, got %d", len(findings))
	}
}

func TestParseGosecOutputJSON(t *testing.T) {
	gosecJSON := `{
		"Issues": [
			{
				"severity": "HIGH",
				"confidence": "HIGH",
				"rule_id": "G101",
				"details": "Potential hardcoded credentials",
				"file": "config.go",
				"line": "42",
				"column": "10"
			},
			{
				"severity": "MEDIUM",
				"confidence": "LOW",
				"rule_id": "G304",
				"details": "File path from variable",
				"file": "handler.go",
				"line": "15",
				"column": "3"
			},
			{
				"severity": "LOW",
				"confidence": "MEDIUM",
				"rule_id": "G401",
				"details": "Use of weak crypto",
				"file": "util.go",
				"line": "7",
				"column": "1"
			}
		]
	}`

	findings := ParseGosecOutput(gosecJSON)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}

	if findings[0].Severity != SeverityCritical {
		t.Errorf("HIGH should map to critical, got %s", findings[0].Severity)
	}
	if findings[0].Rule != "G101" {
		t.Errorf("expected rule G101, got %s", findings[0].Rule)
	}

	if findings[1].Severity != SeverityError {
		t.Errorf("MEDIUM should map to error, got %s", findings[1].Severity)
	}
	if findings[1].File != "handler.go" {
		t.Errorf("expected file handler.go, got %s", findings[1].File)
	}

	if findings[2].Severity != SeverityWarning {
		t.Errorf("LOW should map to warning, got %s", findings[2].Severity)
	}
}

func TestParseGosecOutputRawText(t *testing.T) {
	findings := ParseGosecOutput("some gosec output")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding from raw text, got %d", len(findings))
	}
	if findings[0].Tool != "gosec" {
		t.Errorf("expected tool gosec, got %s", findings[0].Tool)
	}
}

func TestParseGosecOutputEmpty(t *testing.T) {
	findings := ParseGosecOutput("")
	if len(findings) != 0 {
		t.Errorf("expected 0 findings from empty output, got %d", len(findings))
	}
}

func TestParseTestOutputPassed(t *testing.T) {
	output := "ok  github.com/example/pkg 0.003s"
	findings := ParseTestOutput(output, true)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when tests pass, got %d", len(findings))
	}
}

func TestParseTestOutputFailed(t *testing.T) {
	output := `--- FAIL: TestSomething (0.00s)
    thing_test.go:15: expected true, got false
FAIL	github.com/example/pkg	0.003s
FAIL`

	findings := ParseTestOutput(output, false)
	if len(findings) == 0 {
		t.Fatal("expected findings for failed tests")
	}

	foundFail := false
	for _, f := range findings {
		if f.Tool != "go-test" {
			t.Errorf("expected tool go-test, got %s", f.Tool)
		}
		if f.Severity != SeverityError {
			t.Errorf("expected error severity, got %s", f.Severity)
		}
		if containsStr(f.Message, "FAIL") {
			foundFail = true
		}
	}
	if !foundFail {
		t.Error("expected at least one finding containing FAIL")
	}
}

func TestParseTestOutputFailedNoFAILLine(t *testing.T) {
	// Test output without explicit FAIL line but marked as failed
	output := "exit status 1"
	findings := ParseTestOutput(output, false)
	if len(findings) == 0 {
		t.Fatal("expected at least one finding for failed tests")
	}
	if findings[0].Tool != "go-test" {
		t.Errorf("expected tool go-test, got %s", findings[0].Tool)
	}
}

func TestComputeBinaryHash(t *testing.T) {
	data := []byte("hello world")
	hash := ComputeBinaryHash(data)

	if len(hash) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(hash))
	}

	// Known SHA-256 for "hello world"
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("hash mismatch: got %s, want %s", hash, expected)
	}

	// Same input should produce same hash
	hash2 := ComputeBinaryHash(data)
	if hash != hash2 {
		t.Error("same input should produce same hash")
	}

	// Different input should produce different hash
	hash3 := ComputeBinaryHash([]byte("different"))
	if hash == hash3 {
		t.Error("different input should produce different hash")
	}
}

func TestAnalysisSeverityConstants(t *testing.T) {
	if SeverityInfo != "info" {
		t.Errorf("SeverityInfo = %q, want %q", SeverityInfo, "info")
	}
	if SeverityWarning != "warning" {
		t.Errorf("SeverityWarning = %q, want %q", SeverityWarning, "warning")
	}
	if SeverityError != "error" {
		t.Errorf("SeverityError = %q, want %q", SeverityError, "error")
	}
	if SeverityCritical != "critical" {
		t.Errorf("SeverityCritical = %q, want %q", SeverityCritical, "critical")
	}
}

func TestAnalysisFindingJSON(t *testing.T) {
	finding := AnalysisFinding{
		Tool:     "gosec",
		Severity: SeverityCritical,
		File:     "config.go",
		Line:     42,
		Column:   10,
		Message:  "hardcoded credentials",
		Rule:     "G101",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded AnalysisFinding
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Tool != finding.Tool || decoded.Severity != finding.Severity || decoded.Rule != finding.Rule {
		t.Errorf("roundtrip mismatch: got %+v", decoded)
	}
}

func TestAnalysisResultJSON(t *testing.T) {
	result := &AnalysisResult{
		ProposalID:     "prop-123",
		TestPassed:     true,
		LintPassed:     true,
		SecurityPassed: false,
		BuildPassed:    true,
		Passed:         false,
		FailureReason:  "high severity findings detected",
		BinaryHash:     "abc123",
		Duration:       5 * time.Second,
		CompletedAt:    time.Now().UTC(),
		Findings: []AnalysisFinding{
			{Tool: "gosec", Severity: SeverityCritical, Message: "hardcoded creds"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded AnalysisResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ProposalID != "prop-123" {
		t.Errorf("proposal ID mismatch: %s", decoded.ProposalID)
	}
	if decoded.Passed {
		t.Error("expected Passed to be false")
	}
	if len(decoded.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(decoded.Findings))
	}
}

func TestAnalysisResultAllPassed(t *testing.T) {
	result := &AnalysisResult{
		TestPassed:     true,
		LintPassed:     true,
		SecurityPassed: true,
		BuildPassed:    true,
		Passed:         true,
	}
	if result.HasHighSeverity() {
		t.Error("no findings should not be high severity")
	}
	if !result.Passed {
		t.Error("all passed should yield Passed=true")
	}
}

func TestAnalysisResultEmptySummary(t *testing.T) {
	ar := &AnalysisResult{}
	summary := ar.SummaryByTool()
	if len(summary) != 0 {
		t.Errorf("expected empty summary, got %d entries", len(summary))
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'builder', 'analysis_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"analysis_test.go: {len(code)} bytes -> {outpath}")
