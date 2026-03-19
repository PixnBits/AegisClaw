#!/usr/bin/env python3
"""Writes internal/wizard/wizard.go — interactive proposal wizard using charmbracelet/huh."""
import os

code = r'''package wizard

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
)

var (
	// hostRegex validates hostnames/IPs for network policy.
	hostRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.\-:]{0,253}[a-zA-Z0-9]$`)

	// secretNameRegex validates secret reference names.
	secretNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`)

	// skillNameRegex validates skill names (lowercase, starts with letter).
	skillNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,62}$`)
)

// WizardResult contains all collected data from the interactive wizard.
type WizardResult struct {
	// Core proposal fields
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	SkillName   string `json:"skill_name"`

	// Risk assessment
	Risk           string `json:"risk"`
	DataSensitivity int   `json:"data_sensitivity"` // 1-5
	NetworkExposure int   `json:"network_exposure"`  // 1-5
	PrivilegeLevel  int   `json:"privilege_level"`   // 1-5

	// Network policy
	NeedsNetwork     bool     `json:"needs_network"`
	AllowedHosts     []string `json:"allowed_hosts,omitempty"`
	AllowedPorts     []uint16 `json:"allowed_ports,omitempty"`
	AllowedProtocols []string `json:"allowed_protocols,omitempty"`

	// Secrets
	SecretsRefs []string `json:"secrets_refs,omitempty"`

	// Personas
	RequiredPersonas []string `json:"required_personas"`

	// Tools
	Tools []WizardToolSpec `json:"tools"`
}

// WizardToolSpec defines a tool the skill will provide.
type WizardToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ComputedRisk returns the risk level based on the three slider values.
func (r *WizardResult) ComputedRisk() string {
	avg := float64(r.DataSensitivity+r.NetworkExposure+r.PrivilegeLevel) / 3.0
	switch {
	case avg <= 1.5:
		return "low"
	case avg <= 3.0:
		return "medium"
	case avg <= 4.0:
		return "high"
	default:
		return "critical"
	}
}

// RunWizard runs the interactive proposal wizard and returns the collected data.
// The skillGoal is the initial skill description (e.g. "Slack API").
func RunWizard(skillGoal string) (*WizardResult, error) {
	result := &WizardResult{
		Category:         "new_skill",
		RequiredPersonas: []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
	}

	// ---------- Group 1: Skill Goal & Identity ----------
	if err := runGoalForm(result, skillGoal); err != nil {
		return nil, fmt.Errorf("goal form: %w", err)
	}

	// ---------- Group 2: Clarification Questions ----------
	if err := runClarificationForm(result); err != nil {
		return nil, fmt.Errorf("clarification form: %w", err)
	}

	// ---------- Group 3: Risk Assessment ----------
	if err := runRiskForm(result); err != nil {
		return nil, fmt.Errorf("risk form: %w", err)
	}

	// ---------- Group 4: Network Policy ----------
	if err := runNetworkForm(result); err != nil {
		return nil, fmt.Errorf("network form: %w", err)
	}

	// ---------- Group 5: Secrets ----------
	if err := runSecretsForm(result); err != nil {
		return nil, fmt.Errorf("secrets form: %w", err)
	}

	// ---------- Group 6: Personas ----------
	if err := runPersonaForm(result); err != nil {
		return nil, fmt.Errorf("persona form: %w", err)
	}

	// ---------- Group 7: Tools ----------
	if err := runToolsForm(result); err != nil {
		return nil, fmt.Errorf("tools form: %w", err)
	}

	// ---------- Group 8: Confirmation ----------
	if err := runConfirmForm(result); err != nil {
		return nil, fmt.Errorf("confirmation form: %w", err)
	}

	// Set computed risk
	result.Risk = result.ComputedRisk()
	return result, nil
}

func runGoalForm(result *WizardResult, skillGoal string) error {
	defaultName := strings.ToLower(strings.ReplaceAll(skillGoal, " ", "-"))
	defaultName = strings.TrimRight(defaultName, "-")
	// Sanitize to match skill name pattern
	sanitized := make([]byte, 0, len(defaultName))
	for _, b := range []byte(defaultName) {
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			sanitized = append(sanitized, b)
		}
	}
	defaultName = string(sanitized)
	if len(defaultName) > 62 {
		defaultName = defaultName[:62]
	}

	result.SkillName = defaultName
	result.Title = fmt.Sprintf("Add %s skill", skillGoal)
	result.Description = fmt.Sprintf("Implement a new skill for %s integration", skillGoal)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("AegisClaw Proposal Wizard").
				Description(fmt.Sprintf("Creating proposal for skill: %s", skillGoal)),
			huh.NewInput().
				Title("Skill Name").
				Description("Lowercase identifier for the skill (letters, digits, hyphens)").
				Value(&result.SkillName).
				Validate(func(s string) error {
					if !skillNameRegex.MatchString(s) {
						return fmt.Errorf("must match %s", skillNameRegex.String())
					}
					return nil
				}),
			huh.NewInput().
				Title("Proposal Title").
				Description("Short summary of what this proposal does").
				Value(&result.Title).
				Validate(func(s string) error {
					if len(s) == 0 {
						return fmt.Errorf("title is required")
					}
					if len(s) > 200 {
						return fmt.Errorf("title must be <= 200 characters")
					}
					return nil
				}),
			huh.NewText().
				Title("Description").
				Description("Detailed description of the skill's purpose and behavior").
				Value(&result.Description).
				CharLimit(2048).
				Validate(func(s string) error {
					if len(s) == 0 {
						return fmt.Errorf("description is required")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Category").
				Options(
					huh.NewOption("New Skill", "new_skill"),
					huh.NewOption("Edit Skill", "edit_skill"),
					huh.NewOption("Delete Skill", "delete_skill"),
					huh.NewOption("Kernel Patch", "kernel_patch"),
					huh.NewOption("Config Change", "config_change"),
				).
				Value(&result.Category),
		).Title("Skill Identity"),
	)

	return form.Run()
}

func runClarificationForm(result *WizardResult) error {
	var (
		externalAPIs string
		dataHandled  string
		frequency    string
		failureMode  string
		dependencies string
	)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Clarification Questions").
				Description("Help the Court understand the skill's requirements"),
			huh.NewText().
				Title("What external APIs or services will this skill interact with?").
				Description("List endpoints, domains, or service names").
				Value(&externalAPIs),
			huh.NewText().
				Title("What kind of data will this skill handle?").
				Description("e.g., user credentials, public data, PII, financial records").
				Value(&dataHandled),
			huh.NewSelect[string]().
				Title("How frequently will this skill run?").
				Options(
					huh.NewOption("On-demand (user triggered)", "on-demand"),
					huh.NewOption("Periodic (scheduled)", "periodic"),
					huh.NewOption("Continuous (long-running)", "continuous"),
					huh.NewOption("Event-driven (webhook/trigger)", "event-driven"),
				).
				Value(&frequency),
			huh.NewText().
				Title("What should happen when this skill fails?").
				Description("Retry strategy, fallback behavior, alerting").
				Value(&failureMode),
			huh.NewText().
				Title("What other skills or services does this depend on?").
				Description("Leave empty if standalone").
				Value(&dependencies),
		).Title("Clarification"),
	)

	if err := form.Run(); err != nil {
		return err
	}

	// Enrich the description with clarification answers
	var enriched strings.Builder
	enriched.WriteString(result.Description)
	enriched.WriteString("\n\n--- Clarification ---\n")
	if externalAPIs != "" {
		enriched.WriteString(fmt.Sprintf("External APIs: %s\n", externalAPIs))
	}
	if dataHandled != "" {
		enriched.WriteString(fmt.Sprintf("Data handled: %s\n", dataHandled))
	}
	enriched.WriteString(fmt.Sprintf("Frequency: %s\n", frequency))
	if failureMode != "" {
		enriched.WriteString(fmt.Sprintf("Failure mode: %s\n", failureMode))
	}
	if dependencies != "" {
		enriched.WriteString(fmt.Sprintf("Dependencies: %s\n", dependencies))
	}

	result.Description = enriched.String()
	return nil
}

func runRiskForm(result *WizardResult) error {
	dataSensStr := "3"
	networkExpStr := "3"
	privLevelStr := "3"

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Risk Assessment").
				Description("Rate each dimension from 1 (lowest) to 5 (highest)"),
			huh.NewSelect[string]().
				Title("Data Sensitivity").
				Description("How sensitive is the data this skill processes?").
				Options(
					huh.NewOption("1 - Public/non-sensitive", "1"),
					huh.NewOption("2 - Internal/low sensitivity", "2"),
					huh.NewOption("3 - Confidential/moderate", "3"),
					huh.NewOption("4 - Sensitive/PII", "4"),
					huh.NewOption("5 - Critical/secrets/credentials", "5"),
				).
				Value(&dataSensStr),
			huh.NewSelect[string]().
				Title("Network Exposure").
				Description("How much network access does this skill need?").
				Options(
					huh.NewOption("1 - No network access", "1"),
					huh.NewOption("2 - Local only (localhost)", "2"),
					huh.NewOption("3 - Limited external (few hosts)", "3"),
					huh.NewOption("4 - Broad external access", "4"),
					huh.NewOption("5 - Unrestricted network", "5"),
				).
				Value(&networkExpStr),
			huh.NewSelect[string]().
				Title("Privilege Level").
				Description("What system access does this skill require?").
				Options(
					huh.NewOption("1 - Read-only workspace", "1"),
					huh.NewOption("2 - Read-write workspace", "2"),
					huh.NewOption("3 - Workspace + secrets", "3"),
					huh.NewOption("4 - Workspace + secrets + IPC", "4"),
					huh.NewOption("5 - Full system access", "5"),
				).
				Value(&privLevelStr),
		).Title("Risk Sliders"),
	)

	if err := form.Run(); err != nil {
		return err
	}

	result.DataSensitivity, _ = strconv.Atoi(dataSensStr)
	result.NetworkExposure, _ = strconv.Atoi(networkExpStr)
	result.PrivilegeLevel, _ = strconv.Atoi(privLevelStr)
	return nil
}

func runNetworkForm(result *WizardResult) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Does this skill need network access?").
				Description("Default deny is always enforced. Only explicitly allowed hosts are reachable.").
				Value(&result.NeedsNetwork),
		).Title("Network Policy"),
	)

	if err := form.Run(); err != nil {
		return err
	}

	if !result.NeedsNetwork {
		return nil
	}

	var hostsRaw string
	var portsRaw string
	var protocols []string

	form2 := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Allowed Hosts").
				Description("One hostname or IP per line (e.g., api.slack.com)").
				Value(&hostsRaw).
				Validate(func(s string) error {
					for _, line := range strings.Split(s, "\n") {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}
						if !hostRegex.MatchString(line) {
							return fmt.Errorf("invalid host: %q", line)
						}
					}
					return nil
				}),
			huh.NewInput().
				Title("Allowed Ports").
				Description("Comma-separated port numbers (e.g., 443,8080)").
				Value(&portsRaw).
				Validate(func(s string) error {
					for _, part := range strings.Split(s, ",") {
						part = strings.TrimSpace(part)
						if part == "" {
							continue
						}
						n, err := strconv.Atoi(part)
						if err != nil || n < 1 || n > 65535 {
							return fmt.Errorf("invalid port: %q", part)
						}
					}
					return nil
				}),
			huh.NewMultiSelect[string]().
				Title("Allowed Protocols").
				Options(
					huh.NewOption("TCP", "tcp"),
					huh.NewOption("UDP", "udp"),
					huh.NewOption("ICMP", "icmp"),
				).
				Value(&protocols),
		).Title("Network Details"),
	)

	if err := form2.Run(); err != nil {
		return err
	}

	// Parse hosts
	for _, line := range strings.Split(hostsRaw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result.AllowedHosts = append(result.AllowedHosts, line)
		}
	}

	// Parse ports
	for _, part := range strings.Split(portsRaw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, _ := strconv.Atoi(part)
		result.AllowedPorts = append(result.AllowedPorts, uint16(n))
	}

	result.AllowedProtocols = protocols
	return nil
}

func runSecretsForm(result *WizardResult) error {
	var needsSecrets bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Does this skill need secrets (API keys, tokens)?").
				Description("Secrets are encrypted at rest and injected via tmpfs at runtime.").
				Value(&needsSecrets),
		).Title("Secrets"),
	)

	if err := form.Run(); err != nil {
		return err
	}

	if !needsSecrets {
		return nil
	}

	var secretsRaw string
	form2 := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Secret Names").
				Description("One secret name per line (e.g., SLACK_API_TOKEN)").
				Value(&secretsRaw).
				Validate(func(s string) error {
					for _, line := range strings.Split(s, "\n") {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}
						if !secretNameRegex.MatchString(line) {
							return fmt.Errorf("invalid secret name: %q (must match %s)", line, secretNameRegex.String())
						}
					}
					return nil
				}),
		).Title("Secret References"),
	)

	if err := form2.Run(); err != nil {
		return err
	}

	for _, line := range strings.Split(secretsRaw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result.SecretsRefs = append(result.SecretsRefs, line)
		}
	}
	return nil
}

func runPersonaForm(result *WizardResult) error {
	allPersonas := []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Required Court Personas").
				Description("Select which personas must review this proposal").
				Options(
					huh.NewOption("CISO (security oversight)", "CISO"),
					huh.NewOption("Senior Coder (code quality)", "SeniorCoder"),
					huh.NewOption("Security Architect (design)", "SecurityArchitect"),
					huh.NewOption("Tester (test coverage)", "Tester"),
					huh.NewOption("User Advocate (usability)", "UserAdvocate"),
				).
				Value(&result.RequiredPersonas),
		).Title("Court Review Personas"),
	)

	_ = allPersonas
	return form.Run()
}

func runToolsForm(result *WizardResult) error {
	var toolName, toolDesc string
	var addMore bool

	for {
		toolName = ""
		toolDesc = ""
		addMore = false

		prefix := "Define the first tool this skill provides"
		if len(result.Tools) > 0 {
			prefix = fmt.Sprintf("Tool %d defined. Add another?", len(result.Tools))
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewNote().
					Title("Skill Tools").
					Description(prefix),
				huh.NewInput().
					Title("Tool Name").
					Description("Function name (e.g., send_message, query_data)").
					Value(&toolName).
					Validate(func(s string) error {
						if len(s) == 0 {
							return fmt.Errorf("tool name is required")
						}
						return nil
					}),
				huh.NewText().
					Title("Tool Description").
					Description("What does this tool do?").
					Value(&toolDesc).
					Validate(func(s string) error {
						if len(s) == 0 {
							return fmt.Errorf("tool description is required")
						}
						return nil
					}),
			).Title("Tool Definition"),
		)

		if err := form.Run(); err != nil {
			return err
		}

		result.Tools = append(result.Tools, WizardToolSpec{
			Name:        toolName,
			Description: toolDesc,
		})

		moreForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Add another tool?").
					Value(&addMore),
			),
		)

		if err := moreForm.Run(); err != nil {
			return err
		}

		if !addMore {
			break
		}
	}
	return nil
}

func runConfirmForm(result *WizardResult) error {
	summary := formatSummary(result)

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Proposal Summary").
				Description(summary),
			huh.NewConfirm().
				Title("Create this proposal?").
				Affirmative("Yes, create").
				Negative("Cancel").
				Value(&confirmed),
		).Title("Confirm"),
	)

	if err := form.Run(); err != nil {
		return err
	}

	if !confirmed {
		return fmt.Errorf("proposal cancelled by user")
	}
	return nil
}

func formatSummary(result *WizardResult) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Title: %s\n", result.Title))
	b.WriteString(fmt.Sprintf("Skill: %s\n", result.SkillName))
	b.WriteString(fmt.Sprintf("Category: %s\n", result.Category))
	b.WriteString(fmt.Sprintf("Risk: %s (data=%d net=%d priv=%d)\n",
		result.ComputedRisk(), result.DataSensitivity, result.NetworkExposure, result.PrivilegeLevel))

	if result.NeedsNetwork {
		b.WriteString(fmt.Sprintf("Network: %s on ports %v (%v)\n",
			strings.Join(result.AllowedHosts, ", "),
			result.AllowedPorts,
			strings.Join(result.AllowedProtocols, ", ")))
	} else {
		b.WriteString("Network: none (full isolation)\n")
	}

	if len(result.SecretsRefs) > 0 {
		b.WriteString(fmt.Sprintf("Secrets: %s\n", strings.Join(result.SecretsRefs, ", ")))
	}

	b.WriteString(fmt.Sprintf("Personas: %s\n", strings.Join(result.RequiredPersonas, ", ")))

	b.WriteString(fmt.Sprintf("Tools (%d):\n", len(result.Tools)))
	for _, t := range result.Tools {
		b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
	}

	return b.String()
}

// ToProposalJSON produces a JSON representation suitable for creating a Proposal.
// It includes the SkillSpec as the Spec field and network policy + secrets refs.
func (r *WizardResult) ToProposalJSON() (json.RawMessage, error) {
	spec := map[string]interface{}{
		"name":        r.SkillName,
		"description": r.Description,
		"language":    "go",
		"entry_point": fmt.Sprintf("cmd/%s/main.go", r.SkillName),
		"network_policy": map[string]interface{}{
			"default_deny":      true,
			"allowed_hosts":     r.AllowedHosts,
			"allowed_ports":     r.AllowedPorts,
			"allowed_protocols": r.AllowedProtocols,
		},
		"secrets_refs":         r.SecretsRefs,
		"persona_requirements": r.RequiredPersonas,
	}

	tools := make([]map[string]string, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, map[string]string{
			"name":          t.Name,
			"description":   t.Description,
			"input_schema":  "{}",
			"output_schema": "{}",
		})
	}
	spec["tools"] = tools

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal skill spec: %w", err)
	}
	return data, nil
}

// ToNetworkPolicy returns a ProposalNetworkPolicy if network access is needed.
func (r *WizardResult) ToNetworkPolicy() map[string]interface{} {
	return map[string]interface{}{
		"default_deny":      true,
		"allowed_hosts":     r.AllowedHosts,
		"allowed_ports":     r.AllowedPorts,
		"allowed_protocols": r.AllowedProtocols,
	}
}
'''

outdir = os.path.join(os.path.dirname(__file__), '..', 'internal', 'wizard')
outdir = os.path.abspath(outdir)
os.makedirs(outdir, exist_ok=True)
outpath = os.path.join(outdir, 'wizard.go')
with open(outpath, 'w') as f:
    f.write(code)
print(f"wizard.go: {len(code)} bytes -> {outpath}")
