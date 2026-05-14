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

// makeDashboardPRListHandler returns a handler for listing PRs in the dashboard.
func makeDashboardPRListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		// Context unused but required by Handler signature. May be used for
		// cancellation/timeout in future versions.
		_ = ctx
		if env.PRStore == nil {
			return &api.Response{Error: "PR store is unavailable"}
		}

		var req struct {
			Status string `json:"status"` // Optional filter: "open", "merged", "closed"
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		var statusFilter *pullrequest.Status
		if req.Status != "" {
			s := pullrequest.Status(req.Status)
			statusFilter = &s
		}

		prs, err := env.PRStore.List(statusFilter)
		if err != nil {
			return &api.Response{Error: "failed to list PRs: " + err.Error()}
		}

		// Convert to dashboard format
		summaries := make([]dashboardPRSummary, len(prs))
		for i, pr := range prs {
			summaries[i] = dashboardPRSummary{
				ID:                  pr.ID,
				ProposalID:          pr.ProposalID,
				Title:               pr.Title,
				Status:              string(pr.Status),
				Author:              pr.Author,
				Branch:              pr.Branch,
				BaseBranch:          pr.BaseBranch,
				CommitHash:          pr.CommitHash,
				CommitHashShort:     truncateHash(pr.CommitHash, 12),
				CreatedAt:           pr.CreatedAt.Format("2006-01-02 15:04:05"),
				UpdatedAt:           pr.UpdatedAt.Format("2006-01-02 15:04:05"),
				FilesChanged:        pr.FilesChanged,
				Additions:           pr.Additions,
				Deletions:           pr.Deletions,
				BuildPassed:         pr.BuildPassed,
				AnalysisPassed:      pr.AnalysisPassed,
				SecurityGatesPassed: pr.SecurityGatesPassed,
				CourtReviewStatus:   string(pr.CourtReviewStatus),
				CourtReviewCount:    len(pr.CourtReviews),
				Approved:            pr.Approved,
				CanMerge:            pr.CanMerge(),
			}
			if !pr.MergedAt.IsZero() {
				summaries[i].MergedAt = pr.MergedAt.Format("2006-01-02 15:04:05")
			}
		}

		respData, err := json.Marshal(map[string]interface{}{
			"prs":   summaries,
			"total": len(summaries),
		})
		if err != nil {
			return &api.Response{Error: "failed to marshal response: " + err.Error()}
		}

		return &api.Response{Success: true, Data: respData}
	}
}

// makeDashboardPRDetailHandler returns a handler for viewing a single PR's details.
func makeDashboardPRDetailHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		// Context unused but required by Handler signature. May be used for
		// cancellation/timeout in future versions.
		_ = ctx
		if env.PRStore == nil {
			return &api.Response{Error: "PR store is unavailable"}
		}

		var req struct {
			ID         string `json:"id"`
			ProposalID string `json:"proposal_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		req.ID = strings.TrimSpace(req.ID)
		req.ProposalID = strings.TrimSpace(req.ProposalID)

		var pr *pullrequest.PullRequest
		var err error

		if req.ID != "" {
			pr, err = env.PRStore.Get(req.ID)
		} else if req.ProposalID != "" {
			pr, err = env.PRStore.GetByProposalID(req.ProposalID)
		} else {
			return &api.Response{Error: "either 'id' or 'proposal_id' is required"}
		}

		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return &api.Response{Error: "pull request not found"}
			}
			return &api.Response{Error: fmt.Sprintf("failed to get pull request: %v", err)}
		}

		// Convert to dashboard detail format
		detail := dashboardPRDetail{
			dashboardPRSummary: dashboardPRSummary{
				ID:                  pr.ID,
				ProposalID:          pr.ProposalID,
				Title:               pr.Title,
				Status:              string(pr.Status),
				Author:              pr.Author,
				Branch:              pr.Branch,
				BaseBranch:          pr.BaseBranch,
				CommitHash:          pr.CommitHash,
				CommitHashShort:     truncateHash(pr.CommitHash, 12),
				CreatedAt:           pr.CreatedAt.Format("2006-01-02 15:04:05"),
				UpdatedAt:           pr.UpdatedAt.Format("2006-01-02 15:04:05"),
				FilesChanged:        pr.FilesChanged,
				Additions:           pr.Additions,
				Deletions:           pr.Deletions,
				BuildPassed:         pr.BuildPassed,
				AnalysisPassed:      pr.AnalysisPassed,
				SecurityGatesPassed: pr.SecurityGatesPassed,
				CourtReviewStatus:   string(pr.CourtReviewStatus),
				CourtReviewCount:    len(pr.CourtReviews),
				Approved:            pr.Approved,
				CanMerge:            pr.CanMerge(),
			},
			Description:  pr.Description,
			CourtReviews: pr.CourtReviews,
			ApprovedBy:   pr.ApprovedBy,
		}

		if !pr.MergedAt.IsZero() {
			detail.MergedAt = pr.MergedAt.Format("2006-01-02 15:04:05")
		}
		if !pr.ApprovedAt.IsZero() {
			detail.ApprovedAt = pr.ApprovedAt.Format("2006-01-02 15:04:05")
		}

		respData, err := json.Marshal(detail)
		if err != nil {
			return &api.Response{Error: "failed to marshal response: " + err.Error()}
		}

		return &api.Response{Success: true, Data: respData}
	}
}

// makeDashboardPRStatsHandler returns statistics about PRs for the dashboard.
func makeDashboardPRStatsHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		// Context and data unused but required by Handler signature.
		// Stats endpoint doesn't require input parameters.
		_ = ctx
		_ = data
		if env.PRStore == nil {
			return &api.Response{Error: "PR store is unavailable"}
		}

		// Get all PRs
		allPRs, err := env.PRStore.List(nil)
		if err != nil {
			return &api.Response{Error: "failed to list PRs: " + err.Error()}
		}

		// Calculate stats
		stats := map[string]interface{}{
			"total":                     len(allPRs),
			"open":                      0,
			"merged":                    0,
			"closed":                    0,
			"awaiting_court_review":     0,
			"court_review_approved":     0,
			"court_review_rejected":     0,
			"ready_to_merge":            0,
			"total_files_changed":       0,
			"total_additions":           0,
			"total_deletions":           0,
			"build_passed":              0,
			"analysis_passed":           0,
			"security_gates_passed":     0,
		}

		for _, pr := range allPRs {
			switch pr.Status {
			case pullrequest.StatusOpen:
				stats["open"] = stats["open"].(int) + 1
			case pullrequest.StatusMerged:
				stats["merged"] = stats["merged"].(int) + 1
			case pullrequest.StatusClosed:
				stats["closed"] = stats["closed"].(int) + 1
			}

			switch pr.CourtReviewStatus {
			case pullrequest.CourtReviewPending:
				stats["awaiting_court_review"] = stats["awaiting_court_review"].(int) + 1
			case pullrequest.CourtReviewApproved:
				stats["court_review_approved"] = stats["court_review_approved"].(int) + 1
			case pullrequest.CourtReviewRejected:
				stats["court_review_rejected"] = stats["court_review_rejected"].(int) + 1
			}

			if pr.CanMerge() {
				stats["ready_to_merge"] = stats["ready_to_merge"].(int) + 1
			}

			stats["total_files_changed"] = stats["total_files_changed"].(int) + pr.FilesChanged
			stats["total_additions"] = stats["total_additions"].(int) + pr.Additions
			stats["total_deletions"] = stats["total_deletions"].(int) + pr.Deletions

			if pr.BuildPassed {
				stats["build_passed"] = stats["build_passed"].(int) + 1
			}
			if pr.AnalysisPassed {
				stats["analysis_passed"] = stats["analysis_passed"].(int) + 1
			}
			if pr.SecurityGatesPassed {
				stats["security_gates_passed"] = stats["security_gates_passed"].(int) + 1
			}
		}

		respData, err := json.Marshal(stats)
		if err != nil {
			return &api.Response{Error: "failed to marshal response: " + err.Error()}
		}

		return &api.Response{Success: true, Data: respData}
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
