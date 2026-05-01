# dashboard_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers consumed by the web dashboard portal: skill list (with proposals), proposal feedback rounds, builder templates, and sandbox status.

## Key Types / Functions
- `dashboardSkillInfo` / `dashboardToolInfo` / `dashboardTemplateInfo` — compact JSON shapes for dashboard rendering.
- `dashboardSkillsPayload` — combined payload: `runtime_skills`, `built_in_skills`, `built_in_templates`, `proposals`.
- `proposalRoundFeedback` — per-round review list for the proposal detail view.
- `makeDashboardSkillsHandler(env)` — aggregates skill registry, running VMs, proposals, and templates into a single response.
- `makeDashboardProposalFeedbackHandler(env)` — returns round-by-round court review feedback for a proposal.

## System Fit
Serves the dashboard's main skill management view. Aggregates data from multiple internal stores to avoid round-trips from the portal VM.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/builder`
- `github.com/PixnBits/AegisClaw/internal/proposal`
- `github.com/PixnBits/AegisClaw/internal/sandbox`
