# roles.go

## Purpose
Defines the role-specific configuration for worker agents: system prompts, maximum tool call budgets, and default timeout durations. Each role has a carefully crafted system prompt that enforces scope constraints (e.g., the Researcher role must not write code, the Coder role must not browse unrelated URLs) and mandates structured JSON output for results. These constraints prevent role drift and make worker outputs predictable and parseable.

## Key Types and Functions
- `RolePrompt(role Role) string`: returns the full system prompt for the given role; each prompt:
  - Enforces role-specific scope constraints
  - Mandates structured JSON output with defined fields
  - Instructs the agent to stop and report when the task is complete
- `RoleMaxToolCalls(role Role) int`: per-role tool call budget:
  - `RoleResearcher = 20`
  - `RoleCoder = 30`
  - `RoleSummarizer = 10`
  - `RoleCustom = 15`
- `RoleDefaultTimeoutMins(role Role) int`: per-role default timeout:
  - `RoleResearcher = 30` minutes
  - `RoleCoder = 45` minutes
  - `RoleSummarizer = 15` minutes
  - `RoleCustom = 20` minutes

## Role in the System
The `ReActRunner` in `internal/runtime/exec` uses `RolePrompt` as the system message when initialising a worker agent turn. The orchestrator uses `RoleMaxToolCalls` and `RoleDefaultTimeoutMins` to configure the FSM iteration cap and the VM's timeout timer.

## Dependencies
- `internal/worker`: `Role` type from `store.go`
- Standard library only: `fmt` for prompt string construction
