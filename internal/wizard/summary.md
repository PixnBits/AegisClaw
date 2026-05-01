# Package: wizard

## Overview
The `wizard` package provides an interactive, multi-step terminal wizard for authoring AegisClaw governance proposals. It uses `charmbracelet/huh` forms to guide users through a structured 8-step flow — from describing a skill goal to specifying network requirements, secrets, personas, and tools — and produces a `WizardResult` that serialises directly into a governance proposal JSON spec. Risk is automatically scored from three input dimensions and mapped to a four-tier classification.

## Files
- `wizard.go`: `WizardResult`, `WizardToolSpec`, `RunWizard`, `ComputedRisk`, `ToProposalJSON`, `ToNetworkPolicy`, `formatSummary`, validation regexes
- `wizard_test.go`: Tests for risk computation, JSON serialisation, network policy structure, summary formatting, and all three validation regexes

## Key Abstractions
- `WizardResult`: the complete output of a wizard session; contains all fields needed to create a governance proposal
- `RunWizard(skillGoal)`: the primary entry point; blocks until all form steps are completed or the user cancels
- `ComputedRisk()`: deterministic risk tier from averaged input scores — enables consistent risk classification without per-proposal judgement calls
- `ToProposalJSON()`: single-step serialisation from wizard result to the proposal spec JSON format understood by `internal/proposal`
- Validation regexes: enforce safe hostnames, secret names, and skill names before the proposal is submitted

## System Role
Invoked by the `aegisclaw propose` CLI subcommand. After the wizard completes, its output is passed to `internal/proposal.Store.Create` to create a new governance proposal. The wizard is the primary authoring tool for non-technical operators who want to deploy a skill without writing raw JSON.

## Dependencies
- `github.com/charmbracelet/huh`: interactive terminal form framework
- `encoding/json`: proposal spec serialisation
- `regexp`, `math`, `strings`: validation and risk scoring
