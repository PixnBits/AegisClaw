package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/wizard"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// CLI flags for non-interactive skill proposal creation.
var (
	skillName        string
	skillTitle       string
	skillDescription string
	skillTools       []string // "name:description" pairs
	dataSensitivity  int
	networkExposure  int
	privilegeLevel   int
	allowedHosts     []string
	allowedPortStrs  []string
	allowedProtocols []string
	secretRefs       []string
	autoSubmit       bool
)

var proposeSkillCmd = &cobra.Command{
	Use:   "skill <goal>",
	Short: "Propose a new skill (interactive wizard or CLI flags)",
	Long: `Propose a new skill for court review.

By default, launches an interactive wizard. To skip the wizard, provide
--name and at least one --tool flag.

Interactive:
  aegisclaw propose skill "Slack API"

Non-interactive:
  aegisclaw propose skill "Hello World" \
    --name hello-world \
    --tool "greet:Returns a Hello World greeting message" \
    --data-sensitivity 1 --network-exposure 1 --privilege-level 1

  Add --submit to immediately submit for court review.`,
	Args: cobra.ExactArgs(1),
	RunE: runProposeSkill,
}

func parsePorts(raw []string) []uint16 {
	var ports []uint16
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 && n <= 65535 {
			ports = append(ports, uint16(n))
		}
	}
	return ports
}

func isNonInteractive() bool {
	return skillName != "" || len(skillTools) > 0
}

func buildResultFromFlags(skillGoal string) (*wizard.WizardResult, error) {
	name := skillName
	if name == "" {
		// Derive from goal, same logic as the wizard
		name = strings.ToLower(strings.ReplaceAll(skillGoal, " ", "-"))
		sanitized := make([]byte, 0, len(name))
		for _, b := range []byte(name) {
			if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
				sanitized = append(sanitized, b)
			}
		}
		name = string(sanitized)
		if len(name) > 62 {
			name = name[:62]
		}
	}

	title := skillTitle
	if title == "" {
		title = fmt.Sprintf("Add %s skill", skillGoal)
	}

	desc := skillDescription
	if desc == "" {
		desc = fmt.Sprintf("Implement a new skill for %s integration", skillGoal)
	}

	if len(skillTools) == 0 {
		return nil, fmt.Errorf("at least one --tool flag is required (format: name:description)")
	}

	tools := make([]wizard.WizardToolSpec, 0, len(skillTools))
	for _, t := range skillTools {
		parts := strings.SplitN(t, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --tool %q: expected name:description", t)
		}
		tools = append(tools, wizard.WizardToolSpec{Name: parts[0], Description: parts[1]})
	}

	ds := dataSensitivity
	if ds < 1 || ds > 5 {
		ds = 1
	}
	ne := networkExposure
	if ne < 1 || ne > 5 {
		ne = 1
	}
	pl := privilegeLevel
	if pl < 1 || pl > 5 {
		pl = 1
	}

	result := &wizard.WizardResult{
		Title:            title,
		Description:      desc,
		Category:         "new_skill",
		SkillName:        name,
		DataSensitivity:  ds,
		NetworkExposure:  ne,
		PrivilegeLevel:   pl,
		NeedsNetwork:     len(allowedHosts) > 0,
		AllowedHosts:     allowedHosts,
		AllowedPorts:     parsePorts(allowedPortStrs),
		AllowedProtocols: allowedProtocols,
		SecretsRefs:      secretRefs,
		RequiredPersonas: []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
		Tools:            tools,
	}
	result.Risk = result.ComputedRisk()
	return result, nil
}

func runProposeSkill(cmd *cobra.Command, args []string) error {
	skillGoal := args[0]

	var result *wizard.WizardResult
	var err error

	if isNonInteractive() {
		result, err = buildResultFromFlags(skillGoal)
	} else {
		result, err = wizard.RunWizard(skillGoal)
	}
	if err != nil {
		return fmt.Errorf("proposal setup: %w", err)
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

	if autoSubmit {
		if err := p.Transition(proposal.StatusSubmitted, "submitted for review", "operator"); err != nil {
			return fmt.Errorf("cannot submit: %w", err)
		}
		if err := env.ProposalStore.Update(p); err != nil {
			return fmt.Errorf("failed to persist submission: %w", err)
		}

		submitPayload, _ := json.Marshal(map[string]string{"proposal_id": p.ID})
		submitAction := kernel.NewAction(kernel.ActionProposalSubmit, "operator", submitPayload)
		if _, signErr := env.Kernel.SignAndLog(submitAction); signErr != nil {
			env.Logger.Error("failed to log proposal submission", zap.Error(signErr))
		}

		fmt.Printf("\nProposal submitted for court review.\n")
		fmt.Printf("  Status:   %s\n", p.Status)
		fmt.Printf("\nStart review: aegisclaw court review %s\n", p.ID)
	} else {
		fmt.Printf("\nSubmit for review: aegisclaw propose submit %s\n", p.ID)
	}
	return nil
}
