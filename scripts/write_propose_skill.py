#!/usr/bin/env python3
"""Writes cmd/aegisclaw/propose_skill.go — CLI command for interactive proposal wizard."""
import os

code = r'''package main

import (
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/wizard"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var proposeSkillCmd = &cobra.Command{
	Use:   "skill <goal>",
	Short: "Launch interactive wizard to propose a new skill",
	Long: `Runs an interactive wizard that guides you through:
  1. Skill naming and description
  2. Clarification questions (APIs, data, frequency, failure modes)
  3. Risk assessment sliders (data sensitivity, network exposure, privilege level)
  4. Network policy configuration (allowed hosts, ports, protocols)
  5. Secret references for API keys/tokens
  6. Court persona selection
  7. Tool definitions
  8. Confirmation and proposal creation

Example:
  aegisclaw propose skill "Slack API"
  aegisclaw propose skill "Redis Cache"`,
	Args: cobra.ExactArgs(1),
	RunE: runProposeSkill,
}

func runProposeSkill(cmd *cobra.Command, args []string) error {
	skillGoal := args[0]

	// Run the interactive wizard
	result, err := wizard.RunWizard(skillGoal)
	if err != nil {
		return fmt.Errorf("wizard: %w", err)
	}

	// Initialize runtime for proposal storage and audit logging
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// Create the proposal
	cat := proposal.Category(result.Category)
	p, err := proposal.NewProposal(result.Title, result.Description, cat, "operator")
	if err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	// Set risk level from wizard assessment
	p.Risk = proposal.RiskLevel(result.Risk)

	// Set target skill
	p.TargetSkill = result.SkillName

	// Generate and attach skill spec
	spec, err := result.ToProposalJSON()
	if err != nil {
		return fmt.Errorf("failed to generate spec: %w", err)
	}
	p.Spec = spec

	// Set secrets refs
	p.SecretsRefs = result.SecretsRefs

	// Set network policy
	if result.NeedsNetwork {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
			DefaultDeny:      true,
			AllowedHosts:     result.AllowedHosts,
			AllowedPorts:     result.AllowedPorts,
			AllowedProtocols: result.AllowedProtocols,
		}
	} else {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
			DefaultDeny: true,
		}
	}

	// Persist the proposal
	if err := env.ProposalStore.Create(p); err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	// Audit log via kernel
	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID,
		"title":       p.Title,
		"category":    string(p.Category),
		"skill_name":  result.SkillName,
		"risk":        result.Risk,
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "wizard", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log proposal creation", zap.Error(signErr))
	}

	fmt.Println()
	fmt.Printf("Proposal created successfully.\n")
	fmt.Printf("  ID:       %s\n", p.ID)
	fmt.Printf("  Title:    %s\n", p.Title)
	fmt.Printf("  Skill:    %s\n", p.TargetSkill)
	fmt.Printf("  Category: %s\n", p.Category)
	fmt.Printf("  Risk:     %s\n", p.Risk)
	fmt.Printf("  Status:   %s\n", p.Status)

	if len(p.SecretsRefs) > 0 {
		fmt.Printf("  Secrets:  %v\n", p.SecretsRefs)
	}
	if p.NetworkPolicy != nil && len(p.NetworkPolicy.AllowedHosts) > 0 {
		fmt.Printf("  Network:  %v\n", p.NetworkPolicy.AllowedHosts)
	}

	fmt.Printf("\nSubmit for review: aegisclaw propose submit %s\n", p.ID)
	return nil
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'cmd', 'aegisclaw', 'propose_skill.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"propose_skill.go: {len(code)} bytes -> {outpath}")
