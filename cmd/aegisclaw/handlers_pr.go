package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"go.uber.org/zap"
)

// makePRListHandler returns a handler to list pull requests.
func makePRListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Status *string `json:"status"` // Optional: "open", "merged", "closed"
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		
		var statusFilter *pullrequest.Status
		if req.Status != nil && *req.Status != "" {
			s := pullrequest.Status(*req.Status)
			statusFilter = &s
		}
		
		prs, err := env.PRStore.List(statusFilter)
		if err != nil {
			return &api.Response{Error: "failed to list pull requests: " + err.Error()}
		}
		
		// Convert to response format
		type prSummary struct {
			ID                  string  `json:"id"`
			ProposalID          string  `json:"proposal_id"`
			Title               string  `json:"title"`
			Status              string  `json:"status"`
			Author              string  `json:"author"`
			Branch              string  `json:"branch"`
			CreatedAt           string  `json:"created_at"`
			UpdatedAt           string  `json:"updated_at"`
			BuildPassed         bool    `json:"build_passed"`
			AnalysisPassed      bool    `json:"analysis_passed"`
			SecurityGatesPassed bool    `json:"security_gates_passed"`
			CourtReviewStatus   string  `json:"court_review_status"`
			Approved            bool    `json:"approved"`
			CanMerge            bool    `json:"can_merge"`
		}
		
		summaries := make([]prSummary, len(prs))
		for i, pr := range prs {
			summaries[i] = prSummary{
				ID:                  pr.ID,
				ProposalID:          pr.ProposalID,
				Title:               pr.Title,
				Status:              string(pr.Status),
				Author:              pr.Author,
				Branch:              pr.Branch,
				CreatedAt:           pr.CreatedAt.Format("2006-01-02 15:04:05"),
				UpdatedAt:           pr.UpdatedAt.Format("2006-01-02 15:04:05"),
				BuildPassed:         pr.BuildPassed,
				AnalysisPassed:      pr.AnalysisPassed,
				SecurityGatesPassed: pr.SecurityGatesPassed,
				CourtReviewStatus:   string(pr.CourtReviewStatus),
				Approved:            pr.Approved,
				CanMerge:            pr.CanMerge(),
			}
		}
		
		// Marshal to JSON for api.Response.Data
		respData, err := json.Marshal(summaries)
		if err != nil {
			return &api.Response{Error: "failed to marshal response: " + err.Error()}
		}
		
		return &api.Response{Success: true, Data: respData}
	}
}

// makePRGetHandler returns a handler to get a single pull request.
func makePRGetHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			ID         string `json:"id"`          // PR ID
			ProposalID string `json:"proposal_id"` // Alternative: lookup by proposal ID
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		
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
			return &api.Response{Error: "failed to get pull request: " + err.Error()}
		}
		
		// Marshal to JSON for api.Response.Data
		respData, err := json.Marshal(pr)
		if err != nil {
			return &api.Response{Error: "failed to marshal response: " + err.Error()}
		}
		
		return &api.Response{Success: true, Data: respData}
	}
}

// makePRApproveHandler returns a handler to approve a pull request.
func makePRApproveHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			ID         string `json:"id"`
			ApprovedBy string `json:"approved_by"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		
		if req.ID == "" {
			return &api.Response{Error: "PR ID is required"}
		}
		if req.ApprovedBy == "" {
			req.ApprovedBy = "user" // Default approver
		}
		
		if err := env.PRStore.Approve(req.ID, req.ApprovedBy); err != nil {
			return &api.Response{Error: "failed to approve PR: " + err.Error()}
		}
		
		// Log approval to kernel audit trail
		auditPayload, _ := json.Marshal(map[string]string{
			"pr_id":       req.ID,
			"approved_by": req.ApprovedBy,
		})
		action := kernel.NewAction(kernel.ActionType("pr.approve"), "user", auditPayload)
		if _, err := env.Kernel.SignAndLog(action); err != nil {
			env.Logger.Warn("failed to log PR approval", zap.Error(err))
		}
		
		// Marshal response data
		respData, _ := json.Marshal(map[string]string{
			"message": "Pull request approved",
			"pr_id":   req.ID,
		})
		
		return &api.Response{Success: true, Data: respData}
	}
}

// makePRCloseHandler returns a handler to close a pull request without merging.
func makePRCloseHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		
		if req.ID == "" {
			return &api.Response{Error: "PR ID is required"}
		}
		
		if err := env.PRStore.Close(req.ID); err != nil {
			return &api.Response{Error: "failed to close PR: " + err.Error()}
		}
		
		// Log closure to kernel audit trail
		auditPayload, _ := json.Marshal(map[string]string{
			"pr_id": req.ID,
		})
		action := kernel.NewAction(kernel.ActionType("pr.close"), "user", auditPayload)
		if _, err := env.Kernel.SignAndLog(action); err != nil {
			env.Logger.Warn("failed to log PR closure", zap.Error(err))
		}
		
		// Marshal response data
		respData, _ := json.Marshal(map[string]string{
			"message": "Pull request closed",
			"pr_id":   req.ID,
		})
		
		return &api.Response{Success: true, Data: respData}
	}
}

// makePRMergeHandler returns a handler to merge a pull request.
func makePRMergeHandler(env *runtimeEnv) api.Handler {
return func(ctx context.Context, data json.RawMessage) *api.Response {
var req struct {
ID       string `json:"id"`
MergedBy string `json:"merged_by"`
}
if err := json.Unmarshal(data, &req); err != nil {
return &api.Response{Error: "invalid request: " + err.Error()}
}

if req.ID == "" {
return &api.Response{Error: "PR ID is required"}
}
if req.MergedBy == "" {
req.MergedBy = "user" // Default merger
}

// Get PR to check if it can be merged
pr, err := env.PRStore.Get(req.ID)
if err != nil {
return &api.Response{Error: "failed to get PR: " + err.Error()}
}

// Validate PR can be merged
if !pr.CanMerge() {
reasons := []string{}
if pr.Status != pullrequest.StatusOpen {
reasons = append(reasons, "PR is not open")
}
if !pr.BuildPassed {
reasons = append(reasons, "build has not passed")
}
if !pr.AnalysisPassed {
reasons = append(reasons, "analysis has not passed")
}
if !pr.SecurityGatesPassed {
reasons = append(reasons, "security gates have not passed")
}
if pr.CourtReviewRequired && pr.CourtReviewStatus != pullrequest.CourtReviewApproved {
reasons = append(reasons, "Court review not approved")
}
if !pr.Approved {
reasons = append(reasons, "PR not approved by maintainer")
}

return &api.Response{
Error: fmt.Sprintf("PR cannot be merged: %s", strings.Join(reasons, ", ")),
}
}

// Mark PR as merged in store
if err := env.PRStore.MarkMerged(req.ID); err != nil {
return &api.Response{Error: "failed to mark PR as merged: " + err.Error()}
}

// Log merge to kernel audit trail
auditPayload, _ := json.Marshal(map[string]string{
"pr_id":       req.ID,
"proposal_id": pr.ProposalID,
"merged_by":   req.MergedBy,
"branch":      pr.Branch,
"commit":      pr.CommitHash,
})
action := kernel.NewAction(kernel.ActionType("pr.merge"), req.MergedBy, auditPayload)
if _, err := env.Kernel.SignAndLog(action); err != nil {
env.Logger.Warn("failed to log PR merge", zap.Error(err))
}

// Marshal response data
respData, _ := json.Marshal(map[string]string{
"message": "Pull request merged successfully",
"pr_id":   req.ID,
"branch":  pr.Branch,
})

return &api.Response{Success: true, Data: respData}
}
}
