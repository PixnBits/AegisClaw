# wizard_test.go

## Purpose
Unit tests for the `WizardResult` helper methods and validation logic. Since `RunWizard` requires an interactive terminal, tests focus on the programmatic API: risk computation at all tier boundaries, JSON serialisation of various configurations, network policy structure, and all three validation regular expressions.

## Key Types and Functions
- `TestComputedRisk_AllTiers`: constructs `WizardResult` values at boundary scores (avg=2, 2.1, 3, 3.1, 4, 4.1, 5) and verifies the correct tier string is returned
- `TestToProposalJSON_Valid`: creates a fully populated `WizardResult` and verifies `ToProposalJSON` returns valid JSON with all expected fields (network_policy, secrets_refs, tools, entry_point)
- `TestToProposalJSON_EmptyNetwork`: sets `NeedsNetwork=false` and verifies the generated spec omits allowed_hosts and uses an empty policy
- `TestToProposalJSON_MultipleTools`: verifies multiple `WizardToolSpec` entries appear correctly in the tools array
- `TestToNetworkPolicy`: verifies the returned map contains `default_deny: true` and the provided hosts and ports
- `TestFormatSummary_WithNetwork`: verifies the summary string includes network and secrets info when present
- `TestFormatSummary_NoNetworkNoSecrets`: verifies a minimal summary renders without network or secrets sections
- `TestValidation_HostRegex`: tests valid hostnames/IPs and invalid wildcard patterns
- `TestValidation_SecretNameRegex`: tests valid names and invalid ones (leading digit, path separator)
- `TestValidation_SkillNameRegex`: tests valid snake_case names and invalid ones

## Role in the System
Ensures the wizard produces valid, well-structured governance proposal specs that downstream tooling (proposal store, court dashboard) can parse correctly.

## Dependencies
- `testing`, `encoding/json`
- `internal/wizard`: package under test
