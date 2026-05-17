package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
)

// dashboardPRSummary is the dashboard-specific PR summary format.
type dashboardPRSummary struct {
	ID                  string `json:"id"`
	ProposalID          string `json:"proposal_id"`
	Title               string `json:"title"`
	Status              string `json:"status"`
	Author              string `json:"author"`
	Branch              string `json:"branch"`
	BaseBranch          string `json:"base_branch"`
	CommitHash          string `json:"commit_hash"`
	CommitHashShort     string `json:"commit_hash_short"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	FilesChanged        int    `json:"files_changed"`
	Additions           int    `json:"additions"`
	Deletions           int    `json:"deletions"`
	BuildPassed         bool   `json:"build_passed"`
	AnalysisPassed      bool   `json:"analysis_passed"`
	SecurityGatesPassed bool   `json:"security_gates_passed"`
	CourtReviewStatus   string `json:"court_review_status"`
	CourtReviewCount    int    `json:"court_review_count"`
	Approved            bool   `json:"approved"`
	CanMerge            bool   `json:"can_merge"`
	MergedAt            string `json:"merged_at,omitempty"`
}

// dashboardPRDetail includes full PR details for the detail view.
type dashboardPRDetail struct {
	dashboardPRSummary
	Description  string                    `json:"description"`
	CourtReviews []pullrequest.CourtReview `json:"court_reviews,omitempty"`
	ApprovedBy   string                    `json:"approved_by,omitempty"`
	ApprovedAt   string                    `json:"approved_at,omitempty"`
}

// makeDashboardPRListHandler is stubbed — dashboard.pr operations have moved out of the Host Daemon TCB.
func makeDashboardPRListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "dashboard.pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makeDashboardPRDetailHandler is stubbed — dashboard.pr operations have moved out of the Host Daemon TCB.
func makeDashboardPRDetailHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "dashboard.pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makeDashboardPRStatsHandler is stubbed — dashboard.pr operations have moved out of the Host Daemon TCB.
func makeDashboardPRStatsHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "dashboard.pr operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// truncateHash returns the first n characters of a hash string.
// If the hash is shorter than n, returns the full hash.
func truncateHash(hash string, n int) string {
	if len(hash) <= n {
		return hash
	}
	return hash[:n]
}
