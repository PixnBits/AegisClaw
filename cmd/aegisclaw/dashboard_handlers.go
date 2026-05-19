package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
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

// Note: proposal-related payload types removed after stubbing dashboard handlers (Phase 3 TCB).

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

// Dashboard helper functions removed after stubbing proposal/dashboard handlers (Phase 3 TCB clean-up).
