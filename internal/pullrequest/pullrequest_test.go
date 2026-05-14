package pullrequest

import (
	"testing"
	"time"
)

func TestNewPullRequest(t *testing.T) {
	pr, err := NewPullRequest(
		"proposal-123",
		"Add new skill",
		"user@example.com",
		"proposal-123",
		"abc123",
	)
	
	if err != nil {
		t.Fatalf("NewPullRequest failed: %v", err)
	}
	
	if pr.ID == "" {
		t.Error("PR ID should not be empty")
	}
	if pr.ProposalID != "proposal-123" {
		t.Errorf("Expected proposal ID 'proposal-123', got %s", pr.ProposalID)
	}
	if pr.Status != StatusOpen {
		t.Errorf("Expected status %s, got %s", StatusOpen, pr.Status)
	}
	if pr.BaseBranch != "main" {
		t.Errorf("Expected base branch 'main', got %s", pr.BaseBranch)
	}
	if pr.CourtReviewStatus != CourtReviewPending {
		t.Errorf("Expected court review status %s, got %s", CourtReviewPending, pr.CourtReviewStatus)
	}
}

func TestPullRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		pr      *PullRequest
		wantErr bool
	}{
		{
			name: "valid PR",
			pr: &PullRequest{
				ID:                "550e8400-e29b-41d4-a716-446655440000",
				ProposalID:        "proposal-123",
				Title:             "Test PR",
				Author:            "user",
				Branch:            "proposal-123",
				CommitHash:        "abc123",
				Status:            StatusOpen,
				CourtReviewStatus: CourtReviewPending,
				CreatedAt:         time.Now().UTC(),
				UpdatedAt:         time.Now().UTC(),
			},
			wantErr: false,
		},
		{
			name: "missing title",
			pr: &PullRequest{
				ID:                "550e8400-e29b-41d4-a716-446655440000",
				ProposalID:        "proposal-123",
				Author:            "user",
				Branch:            "proposal-123",
				CommitHash:        "abc123",
				Status:            StatusOpen,
				CourtReviewStatus: CourtReviewPending,
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			pr: &PullRequest{
				ID:                "550e8400-e29b-41d4-a716-446655440000",
				ProposalID:        "proposal-123",
				Title:             "Test PR",
				Author:            "user",
				Branch:            "proposal-123",
				CommitHash:        "abc123",
				Status:            Status("invalid"),
				CourtReviewStatus: CourtReviewPending,
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pr.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanMerge(t *testing.T) {
	tests := []struct {
		name string
		pr   *PullRequest
		want bool
	}{
		{
			name: "can merge - all requirements met",
			pr: &PullRequest{
				Status:              StatusOpen,
				BuildPassed:         true,
				AnalysisPassed:      true,
				SecurityGatesPassed: true,
				CourtReviewRequired: false,
				Approved:            true,
			},
			want: true,
		},
		{
			name: "cannot merge - not approved",
			pr: &PullRequest{
				Status:              StatusOpen,
				BuildPassed:         true,
				AnalysisPassed:      true,
				SecurityGatesPassed: true,
				CourtReviewRequired: false,
				Approved:            false,
			},
			want: false,
		},
		{
			name: "cannot merge - security gates failed",
			pr: &PullRequest{
				Status:              StatusOpen,
				BuildPassed:         true,
				AnalysisPassed:      true,
				SecurityGatesPassed: false,
				CourtReviewRequired: false,
				Approved:            true,
			},
			want: false,
		},
		{
			name: "cannot merge - court review required but not approved",
			pr: &PullRequest{
				Status:              StatusOpen,
				BuildPassed:         true,
				AnalysisPassed:      true,
				SecurityGatesPassed: true,
				CourtReviewRequired: true,
				CourtReviewStatus:   CourtReviewPending,
				Approved:            true,
			},
			want: false,
		},
		{
			name: "can merge - court review approved",
			pr: &PullRequest{
				Status:              StatusOpen,
				BuildPassed:         true,
				AnalysisPassed:      true,
				SecurityGatesPassed: true,
				CourtReviewRequired: true,
				CourtReviewStatus:   CourtReviewApproved,
				Approved:            true,
			},
			want: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pr.CanMerge(); got != tt.want {
				t.Errorf("CanMerge() = %v, want %v", got, tt.want)
			}
		})
	}
}
