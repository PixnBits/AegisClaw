package builder

import (
	"encoding/json"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/proposal"
)

func TestFixRequestValidation(t *testing.T) {
	validSpec := SkillSpec{
		Name:        "test-skill",
		Description: "A test skill",
		Language:    "go",
		EntryPoint:  "main.go",
		Tools:       []ToolSpec{{Name: "test-tool", Description: "A test tool"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	tests := []struct {
		name    string
		req     *FixRequest
		wantErr string
	}{
		{
			"empty proposal ID",
			&FixRequest{
				ProposalID:   "",
				SkillSpec:    validSpec,
				CurrentFiles: map[string]string{"a.go": "x"},
				Feedback:     []ReviewFeedback{{ReviewerPersona: "coder", Verdict: "reject", Comments: "bad"}},
				Round:        2,
			},
			"proposal ID is required",
		},
		{
			"empty files",
			&FixRequest{
				ProposalID: "p-1",
				SkillSpec:  validSpec,
				Feedback:   []ReviewFeedback{{ReviewerPersona: "coder", Verdict: "reject", Comments: "bad"}},
				Round:      2,
			},
			"current files are required",
		},
		{
			"no feedback or analysis",
			&FixRequest{
				ProposalID:   "p-1",
				SkillSpec:    validSpec,
				CurrentFiles: map[string]string{"a.go": "x"},
				Round:        2,
			},
			"feedback or analysis result is required",
		},
		{
			"round too low",
			&FixRequest{
				ProposalID:   "p-1",
				SkillSpec:    validSpec,
				CurrentFiles: map[string]string{"a.go": "x"},
				Feedback:     []ReviewFeedback{{ReviewerPersona: "coder", Verdict: "reject", Comments: "bad"}},
				Round:        1,
			},
			"fix round must be between 2",
		},
		{
			"round too high",
			&FixRequest{
				ProposalID:   "p-1",
				SkillSpec:    validSpec,
				CurrentFiles: map[string]string{"a.go": "x"},
				Feedback:     []ReviewFeedback{{ReviewerPersona: "coder", Verdict: "reject", Comments: "bad"}},
				Round:        10,
			},
			"fix round must be between 2",
		},
		{
			"valid with feedback",
			&FixRequest{
				ProposalID:   "p-1",
				SkillSpec:    validSpec,
				CurrentFiles: map[string]string{"a.go": "x"},
				Feedback:     []ReviewFeedback{{ReviewerPersona: "coder", Verdict: "reject", Comments: "bad"}},
				Round:        2,
			},
			"",
		},
		{
			"valid with analysis only",
			&FixRequest{
				ProposalID:     "p-1",
				SkillSpec:      validSpec,
				CurrentFiles:   map[string]string{"a.go": "x"},
				AnalysisResult: &AnalysisResult{Passed: false, TestPassed: false},
				Round:          2,
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

func TestFeedbackSummaryWithReviewerFeedback(t *testing.T) {
	req := &FixRequest{
		ProposalID: "p-1",
		SkillSpec: SkillSpec{
			Name: "test", Description: "test", Language: "go", EntryPoint: "main.go",
		},
		CurrentFiles: map[string]string{"main.go": "package main\n"},
		Feedback: []ReviewFeedback{
			{
				ReviewerPersona: "Coder",
				Verdict:         "reject",
				Comments:        "Missing error handling in handler",
				Questions:       []string{"Why no context cancellation?"},
				Concerns:        []string{"Risk of goroutine leak"},
			},
			{
				ReviewerPersona: "CISO",
				Verdict:         "reject",
				Comments:        "SQL injection vulnerability",
			},
		},
		Round: 2,
	}

	summary := req.FeedbackSummary()

	if !containsStr(summary, "Fix Round 2") {
		t.Error("summary should mention round 2")
	}
	if !containsStr(summary, "Coder") {
		t.Error("summary should mention Coder reviewer")
	}
	if !containsStr(summary, "CISO") {
		t.Error("summary should mention CISO reviewer")
	}
	if !containsStr(summary, "Missing error handling") {
		t.Error("summary should contain comments")
	}
	if !containsStr(summary, "Why no context cancellation") {
		t.Error("summary should contain questions")
	}
	if !containsStr(summary, "Risk of goroutine leak") {
		t.Error("summary should contain concerns")
	}
	if !containsStr(summary, "SQL injection") {
		t.Error("summary should contain all reviewer comments")
	}
	if !containsStr(summary, "Please fix all issues") {
		t.Error("summary should contain fix instruction")
	}
}

func TestFeedbackSummaryWithAnalysisFailures(t *testing.T) {
	req := &FixRequest{
		ProposalID: "p-2",
		SkillSpec: SkillSpec{
			Name: "test", Description: "test", Language: "go", EntryPoint: "main.go",
		},
		CurrentFiles: map[string]string{"main.go": "package main\n"},
		AnalysisResult: &AnalysisResult{
			Passed:         false,
			TestPassed:     false,
			TestOutput:     "--- FAIL: TestHandler",
			LintPassed:     false,
			LintOutput:     "govet: unreachable code",
			SecurityPassed: true,
			BuildPassed:    true,
			Findings: []AnalysisFinding{
				{Tool: "go-test", Severity: SeverityError, Message: "TestHandler failed"},
				{Tool: "golangci-lint", Severity: SeverityWarning, Message: "unreachable", File: "main.go", Line: 10},
			},
		},
		Round: 2,
	}

	summary := req.FeedbackSummary()

	if !containsStr(summary, "Tests FAILED") {
		t.Error("summary should indicate tests failed")
	}
	if !containsStr(summary, "Lint FAILED") {
		t.Error("summary should indicate lint failed")
	}
	if !containsStr(summary, "TestHandler") {
		t.Error("summary should include test output")
	}
	if !containsStr(summary, "unreachable") {
		t.Error("summary should include lint finding")
	}
}

func TestExtractFeedbackFromReviews(t *testing.T) {
	reviews := []proposal.Review{
		{
			ID:       "r-1",
			Persona:  "Coder",
			Model:    "llama3",
			Round:    1,
			Verdict:  proposal.VerdictReject,
			Comments: "Code does not handle errors properly",
			Questions: []string{"Have you considered using context?"},
			Evidence:  []string{"Missing concern about nil pointer", "Good structure overall"},
		},
		{
			ID:      "r-2",
			Persona: "Architect",
			Model:   "llama3",
			Round:   1,
			Verdict: proposal.VerdictApprove,
			Comments: "LGTM",
		},
		{
			ID:      "r-3",
			Persona: "CISO",
			Model:   "llama3",
			Round:   1,
			Verdict: proposal.VerdictAsk,
			Comments: "Need more detail on security",
			Questions: []string{"What about auth?"},
			Evidence:  []string{"Potential vulnerable endpoint", "Risk of data exposure"},
		},
	}

	feedback := ExtractFeedback(reviews)

	if len(feedback) != 2 {
		t.Fatalf("expected 2 feedback items (reject + ask), got %d", len(feedback))
	}

	// Coder (reject)
	if feedback[0].ReviewerPersona != "Coder" {
		t.Errorf("expected Coder, got %s", feedback[0].ReviewerPersona)
	}
	if feedback[0].Verdict != "reject" {
		t.Errorf("expected reject, got %s", feedback[0].Verdict)
	}
	if !containsStr(feedback[0].Comments, "errors properly") {
		t.Error("expected comments from Coder")
	}
	if len(feedback[0].Questions) != 1 {
		t.Errorf("expected 1 question, got %d", len(feedback[0].Questions))
	}
	// "Missing concern about nil pointer" should match the concern keyword
	if len(feedback[0].Concerns) != 1 {
		t.Errorf("expected 1 concern (matching 'concern' keyword), got %d", len(feedback[0].Concerns))
	}

	// CISO (ask)
	if feedback[1].ReviewerPersona != "CISO" {
		t.Errorf("expected CISO, got %s", feedback[1].ReviewerPersona)
	}
	if feedback[1].Verdict != "ask" {
		t.Errorf("expected ask, got %s", feedback[1].Verdict)
	}
	if len(feedback[1].Concerns) != 2 {
		t.Errorf("expected 2 concerns (vulnerable + risk), got %d", len(feedback[1].Concerns))
	}
}

func TestExtractFeedbackSkipsApproveAndAbstain(t *testing.T) {
	reviews := []proposal.Review{
		{ID: "r-1", Persona: "Coder", Model: "m", Round: 1, Verdict: proposal.VerdictApprove},
		{ID: "r-2", Persona: "Arch", Model: "m", Round: 1, Verdict: proposal.VerdictAbstain},
	}

	feedback := ExtractFeedback(reviews)
	if len(feedback) != 0 {
		t.Errorf("expected 0 feedback for approve/abstain, got %d", len(feedback))
	}
}

func TestNewIterationEngineValidation(t *testing.T) {
	_, err := NewIterationEngine(nil, nil, nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil pipeline")
	}
	if !containsStr(err.Error(), "pipeline is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFixRoundStateConstants(t *testing.T) {
	states := map[FixRoundState]string{
		FixRoundPending:  "pending",
		FixRoundRunning:  "running",
		FixRoundComplete: "complete",
		FixRoundFailed:   "failed",
	}
	for state, expected := range states {
		if string(state) != expected {
			t.Errorf("state %v should be %q", state, expected)
		}
	}
}

func TestMaxFixRoundsConstant(t *testing.T) {
	if MaxFixRounds != 3 {
		t.Errorf("MaxFixRounds should be 3, got %d", MaxFixRounds)
	}
}

func TestReviewFeedbackJSON(t *testing.T) {
	fb := ReviewFeedback{
		ReviewerPersona: "CISO",
		Verdict:         "reject",
		Comments:        "SQL injection risk",
		Questions:       []string{"Are inputs sanitized?"},
		Concerns:        []string{"Unsanitized user input"},
	}

	data, err := json.Marshal(fb)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ReviewFeedback
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ReviewerPersona != "CISO" || decoded.Verdict != "reject" {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
	if len(decoded.Questions) != 1 || decoded.Questions[0] != "Are inputs sanitized?" {
		t.Errorf("questions mismatch: %v", decoded.Questions)
	}
	if len(decoded.Concerns) != 1 {
		t.Errorf("concerns mismatch: %v", decoded.Concerns)
	}
}

func TestIterationResultJSON(t *testing.T) {
	result := &IterationResult{
		ProposalID:  "prop-abc",
		FinalRound:  3,
		FinalPassed: true,
		FinalCommit: "def456",
		Rounds: []FixRound{
			{Round: 2, State: FixRoundComplete, CommitHash: "aaa"},
			{Round: 3, State: FixRoundComplete, CommitHash: "def456"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded IterationResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ProposalID != "prop-abc" || decoded.FinalRound != 3 || !decoded.FinalPassed {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
	if len(decoded.Rounds) != 2 {
		t.Errorf("expected 2 rounds, got %d", len(decoded.Rounds))
	}
}

func TestFixRoundJSON(t *testing.T) {
	fr := FixRound{
		Round:      2,
		State:      FixRoundComplete,
		CommitHash: "abc123",
		Diff:       "--- a/main.go\n+++ b/main.go",
		Files:      map[string]string{"main.go": "package main\n"},
		FeedbackUsed: []ReviewFeedback{
			{ReviewerPersona: "Coder", Verdict: "reject", Comments: "fix this"},
		},
	}

	data, err := json.Marshal(fr)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded FixRound
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Round != 2 || decoded.State != FixRoundComplete {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
	if decoded.CommitHash != "abc123" {
		t.Errorf("commit hash mismatch: %s", decoded.CommitHash)
	}
	if len(decoded.FeedbackUsed) != 1 {
		t.Errorf("expected 1 feedback, got %d", len(decoded.FeedbackUsed))
	}
}
