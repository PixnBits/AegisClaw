# wizard.go

## Purpose
Implements an interactive, multi-step terminal wizard for creating governance proposals using `charmbracelet/huh` forms. The wizard guides users through eight sequential steps — goal, clarification, risk assessment, network configuration, secrets, required personas, tools, and confirmation — and produces a complete `WizardResult` that can be serialised into a governance proposal JSON spec. Risk is automatically computed as the average of data sensitivity, network exposure, and privilege level scores.

## Key Types and Functions
- `WizardResult`: Title, Description, Category, SkillName, Risk, DataSensitivity (1–5), NetworkExposure (1–5), PrivilegeLevel (1–5), NeedsNetwork, AllowedHosts, AllowedPorts, AllowedProtocols, SecretsRefs, RequiredPersonas, Tools (`[]WizardToolSpec`)
- `WizardToolSpec`: Name, Description
- `RunWizard(skillGoal string) (*WizardResult, error)`: runs all 8 huh form steps sequentially; pre-fills goal if provided
- `ComputedRisk() string`: `avg(DataSensitivity, NetworkExposure, PrivilegeLevel)` → `"low"` (≤2), `"medium"` (≤3), `"high"` (≤4), `"critical"` (>4)
- `ToProposalJSON() (string, error)`: serialises the result into the skill spec JSON format with network policy, secrets refs, tools array, and entry point
- `ToNetworkPolicy() map[string]interface{}`: constructs the network policy map from wizard inputs
- Validation regexes: `hostRegex` (hostname/IP), `secretNameRegex` (`^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`), `skillNameRegex` (snake_case)
- `formatSummary(result) string`: renders a human-readable confirmation summary

## Role in the System
The wizard is invoked by the `aegisclaw propose` CLI command. It transforms a natural-language skill goal into a structured governance proposal ready for Governance Court review.

## Dependencies
- `github.com/charmbracelet/huh`: interactive terminal forms
- `encoding/json`: spec serialisation
- `regexp`, `strings`, `math`: validation and risk computation
