package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

const defaultScriptRunnerSkill = "default-script-runner"

// bootstrapDefaultScriptRunner ensures there is at least one baseline proposal
// for a generic scripting runner skill in default setups.
func bootstrapDefaultScriptRunner(ctx context.Context, env *runtimeEnv, engine *court.Engine) {
	if env == nil || env.ProposalStore == nil || engine == nil {
		return
	}
	if env.SafeMode.Load() {
		return
	}
	if _, ok := env.Registry.Get(defaultScriptRunnerSkill); ok {
		return
	}

	summaries, err := env.ProposalStore.List()
	if err != nil {
		env.Logger.Warn("bootstrap: list proposals failed", zap.Error(err))
		return
	}
	for _, s := range summaries {
		if s.TargetSkill == defaultScriptRunnerSkill {
			switch s.Status {
			case proposal.StatusRejected, proposal.StatusWithdrawn, proposal.StatusFailed:
				continue
			default:
				return
			}
		}
	}

	spec := map[string]interface{}{
		"name":        defaultScriptRunnerSkill,
		"description": "Default scripting runner that executes short scripts in approved runtimes with strict time and output limits.",
		"language":    "python",
		"entry_point": "cmd/default-script-runner/main.go",
		"tools": []map[string]string{
			{
				"name":          "execute_script",
				"description":   "Execute short scripts using approved runtimes with timeout and output truncation.",
				"input_schema":  "{}",
				"output_schema": "{}",
			},
		},
		"network_policy": map[string]interface{}{
			"default_deny":      true,
			"allowed_hosts":     []string{},
			"allowed_ports":     []uint16{},
			"allowed_protocols": []string{},
		},
		"persona_requirements": []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
	}
	specJSON, _ := json.Marshal(spec)

	p, err := proposal.NewProposal(
		"Bootstrap default script runner skill",
		"Provide a baseline scripting capability so agents can execute short scripts using approved runtimes without custom per-scenario prompts.",
		proposal.CategoryNewSkill,
		"system",
	)
	if err != nil {
		env.Logger.Warn("bootstrap: create proposal failed", zap.Error(err))
		return
	}
	p.TargetSkill = defaultScriptRunnerSkill
	p.Spec = specJSON
	p.Risk = proposal.RiskMedium
	p.NetworkPolicy = &proposal.ProposalNetworkPolicy{DefaultDeny: true}

	if err := env.ProposalStore.Create(p); err != nil {
		env.Logger.Warn("bootstrap: persist proposal failed", zap.Error(err))
		return
	}
	if err := p.Transition(proposal.StatusSubmitted, "auto bootstrap", "system"); err != nil {
		env.Logger.Warn("bootstrap: transition failed", zap.Error(err))
		return
	}
	if err := env.ProposalStore.Update(p); err != nil {
		env.Logger.Warn("bootstrap: update proposal failed", zap.Error(err))
		return
	}

	env.Logger.Info("bootstrap: submitted default script runner proposal", zap.String("proposal_id", p.ID))
	go func(proposalID string) {
		if _, reviewErr := engine.Review(ctx, proposalID); reviewErr != nil {
			env.Logger.Warn("bootstrap: court review failed", zap.String("proposal_id", proposalID), zap.Error(reviewErr))
			return
		}
		env.Logger.Info("bootstrap: court review complete", zap.String("proposal_id", proposalID))
	}(p.ID)
}
