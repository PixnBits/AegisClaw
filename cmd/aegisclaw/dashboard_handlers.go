package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/builder"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
)

type dashboardToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type dashboardSkillInfo struct {
	Name           string              `json:"name"`
	Description    string              `json:"description,omitempty"`
	State          string              `json:"state"`
	Version        int                 `json:"version,omitempty"`
	SandboxID      string              `json:"sandbox_id,omitempty"`
	Source         string              `json:"source,omitempty"`
	ProposalID     string              `json:"proposal_id,omitempty"`
	ProposalTitle  string              `json:"proposal_title,omitempty"`
	ProposalStatus string              `json:"proposal_status,omitempty"`
	Tools          []dashboardToolInfo `json:"tools,omitempty"`
	Metadata       map[string]string   `json:"metadata,omitempty"`
}

type dashboardTemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

type dashboardSkillsPayload struct {
	RuntimeSkills    []dashboardSkillInfo       `json:"runtime_skills"`
	BuiltInSkills    []dashboardSkillInfo       `json:"built_in_skills"`
	BuiltInTemplates []dashboardTemplateInfo    `json:"built_in_templates"`
	Proposals        []proposal.ProposalSummary `json:"proposals"`
}

type proposalRoundFeedback struct {
	Round   int               `json:"round"`
	Reviews []proposal.Review `json:"reviews"`
}

type dashboardReviewStatus struct {
	Status         string `json:"status"`
	CurrentRound   int    `json:"current_round"`
	CurrentCount   int    `json:"current_count"`
	TotalReviews   int    `json:"total_reviews"`
	PendingReviews int    `json:"pending_reviews"`
	ApprovalCount  int    `json:"approval_count"`
	RejectCount    int    `json:"reject_count"`
	AskCount       int    `json:"ask_count"`
	AbstainCount   int    `json:"abstain_count"`
}

type dashboardProposalDetailPayload struct {
	Proposal             *proposal.Proposal      `json:"proposal"`
	ReviewStatus         dashboardReviewStatus   `json:"review_status"`
	CurrentRoundFeedback []proposal.Review       `json:"current_round_feedback"`
	PreviousRounds       []proposalRoundFeedback `json:"previous_rounds"`
	RevisionHistory      []proposal.StatusChange `json:"revision_history"`
}

type skillProposalDetails struct {
	Summary proposal.ProposalSummary
	Full    *proposal.Proposal
	Spec    *builder.SkillSpec
}

func makeDashboardSkillsHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = ctx

		proposals, err := env.ProposalStore.List()
		if err != nil {
			return &api.Response{Error: "failed to list proposals: " + err.Error()}
		}

		detailsBySkill := make(map[string]skillProposalDetails)
		for _, summary := range proposals {
			full, getErr := env.ProposalStore.Get(summary.ID)
			if getErr != nil || full == nil || full.TargetSkill == "" {
				continue
			}

			candidate := skillProposalDetails{Summary: summary, Full: full}
			if len(full.Spec) > 0 {
				var spec builder.SkillSpec
				if json.Unmarshal(full.Spec, &spec) == nil {
					candidate.Spec = &spec
				}
			}

			existing, ok := detailsBySkill[full.TargetSkill]
			if !ok || summary.UpdatedAt.After(existing.Summary.UpdatedAt) {
				detailsBySkill[full.TargetSkill] = candidate
			}
		}

		registryEntries := env.Registry.List()
		sort.Slice(registryEntries, func(i, j int) bool {
			return registryEntries[i].Name < registryEntries[j].Name
		})

		runtimeSkills := make([]dashboardSkillInfo, 0, len(registryEntries))
		for _, entry := range registryEntries {
			if entry.Name == defaultScriptRunnerSkill {
				continue
			}
			runtimeSkills = append(runtimeSkills, buildDashboardSkillInfo(entry, detailsBySkill[entry.Name], false))
		}

		builtInSkill := buildDefaultBuiltInSkill(env.Registry, detailsBySkill[defaultScriptRunnerSkill])

		templates := builder.DefaultTemplates()
		builtInTemplates := make([]dashboardTemplateInfo, 0, len(templates))
		for _, name := range sortedTemplateNames(templates) {
			tmpl := templates[name]
			builtInTemplates = append(builtInTemplates, dashboardTemplateInfo{
				Name:        tmpl.Name,
				Description: tmpl.Description,
				Kind:        "builder_template",
			})
		}

		payload, _ := json.Marshal(dashboardSkillsPayload{
			RuntimeSkills:    runtimeSkills,
			BuiltInSkills:    []dashboardSkillInfo{builtInSkill},
			BuiltInTemplates: builtInTemplates,
			Proposals:        proposals,
		})
		return &api.Response{Success: true, Data: payload}
	}
}

func makeDashboardProposalHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = ctx
		if env.ProposalStore == nil {
			return &api.Response{Error: "proposal store is unavailable"}
		}

		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" {
			return &api.Response{Error: "proposal id is required"}
		}

		p, err := env.ProposalStore.Get(req.ID)
		if err != nil {
			return &api.Response{Error: fmt.Sprintf("failed to load proposal %s: %v", req.ID, err)}
		}

		current := make([]proposal.Review, 0)
		previousByRound := make(map[int][]proposal.Review)
		status := dashboardReviewStatus{
			Status:       string(p.Status),
			CurrentRound: p.Round,
			TotalReviews: len(p.Reviews),
		}

		for _, review := range p.Reviews {
			if review.Round == p.Round {
				current = append(current, review)
				switch review.Verdict {
				case proposal.VerdictApprove:
					status.ApprovalCount++
				case proposal.VerdictReject:
					status.RejectCount++
				case proposal.VerdictAsk:
					status.AskCount++
				case proposal.VerdictAbstain:
					status.AbstainCount++
				}
				continue
			}
			previousByRound[review.Round] = append(previousByRound[review.Round], review)
		}

		sort.Slice(current, func(i, j int) bool {
			return current[i].Timestamp.Before(current[j].Timestamp)
		})
		status.CurrentCount = len(current)
		if p.Round > 0 {
			status.PendingReviews = 5 - len(current)
			if status.PendingReviews < 0 {
				status.PendingReviews = 0
			}
		}

		rounds := make([]int, 0, len(previousByRound))
		for round := range previousByRound {
			rounds = append(rounds, round)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(rounds)))

		previous := make([]proposalRoundFeedback, 0, len(rounds))
		for _, round := range rounds {
			reviews := previousByRound[round]
			sort.Slice(reviews, func(i, j int) bool {
				return reviews[i].Timestamp.Before(reviews[j].Timestamp)
			})
			previous = append(previous, proposalRoundFeedback{Round: round, Reviews: reviews})
		}

		history := append([]proposal.StatusChange(nil), p.History...)
		sort.Slice(history, func(i, j int) bool {
			return history[i].Timestamp.After(history[j].Timestamp)
		})

		payload, _ := json.Marshal(dashboardProposalDetailPayload{
			Proposal:             p,
			ReviewStatus:         status,
			CurrentRoundFeedback: current,
			PreviousRounds:       previous,
			RevisionHistory:      history,
		})
		return &api.Response{Success: true, Data: payload}
	}
}

func buildDashboardSkillInfo(entry sandbox.SkillEntry, details skillProposalDetails, builtIn bool) dashboardSkillInfo {
	info := dashboardSkillInfo{
		Name:      entry.Name,
		State:     string(entry.State),
		Version:   entry.Version,
		SandboxID: entry.SandboxID,
		Metadata:  entry.Metadata,
		Source:    "user",
	}
	if builtIn {
		info.Source = "built-in baseline"
		if details.Full != nil && details.Full.Author != "" {
			info.Source = "built-in baseline (" + details.Full.Author + ")"
		}
	}
	applySkillProposalDetails(&info, details)
	return info
}

func buildDefaultBuiltInSkill(registry *sandbox.SkillRegistry, details skillProposalDetails) dashboardSkillInfo {
	entry, ok := registry.Get(defaultScriptRunnerSkill)
	if ok && entry != nil {
		return buildDashboardSkillInfo(*entry, details, true)
	}

	info := dashboardSkillInfo{
		Name:        defaultScriptRunnerSkill,
		Description: "Default scripting runner that executes short scripts in approved runtimes with strict time and output limits.",
		State:       "not_bootstrapped",
		Source:      "built-in baseline",
		Tools: []dashboardToolInfo{{
			Name:        "execute_script",
			Description: "Execute short scripts using approved runtimes with timeout and output truncation.",
		}},
	}
	applySkillProposalDetails(&info, details)
	if details.Full != nil {
		info.State = string(details.Full.Status)
		if details.Full.Author != "" {
			info.Source = "built-in baseline (" + details.Full.Author + ")"
		}
	}
	return info
}

func applySkillProposalDetails(info *dashboardSkillInfo, details skillProposalDetails) {
	if info == nil {
		return
	}
	if details.Full != nil {
		info.ProposalID = details.Full.ID
		info.ProposalTitle = details.Full.Title
		info.ProposalStatus = string(details.Full.Status)
		if info.Description == "" && details.Full.Description != "" {
			info.Description = details.Full.Description
		}
	}
	if details.Spec != nil {
		if details.Spec.Description != "" {
			info.Description = details.Spec.Description
		}
		info.Tools = make([]dashboardToolInfo, 0, len(details.Spec.Tools))
		for _, tool := range details.Spec.Tools {
			info.Tools = append(info.Tools, dashboardToolInfo{
				Name:        tool.Name,
				Description: tool.Description,
			})
		}
	}
	if details.Full != nil && details.Full.Author == "system" && info.Source == "user" {
		info.Source = "system"
	}
}

func sortedTemplateNames(templates map[string]*builder.PromptTemplate) []string {
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
