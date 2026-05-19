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
		_ = data
		_ = env
		return &api.Response{Error: "dashboard proposal access removed from Host Daemon TCB (Phase 3)"}
	}
}

func makeDashboardProposalHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = ctx
		_ = data
		_ = env
		return &api.Response{Error: "dashboard proposal access removed from Host Daemon TCB (Phase 3)"}
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
