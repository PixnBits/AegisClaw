# react_journey_test.go — cmd/aegisclaw

## Purpose
Table-driven journey tests for the full agent lifecycle using the real proposal handlers and a scripted `TaskExecutor`. Exercises 12 distinct failure modes and lifecycle paths without KVM or Ollama.

## Key Scenarios
1. Simple single tool use (`create_draft`).
2. Multi-step flow: `create → list → get → submit`.
3. Explicit task completion with no tool call.
4. Unknown tool → rejected, no infinite loop.
5. Tool failure / bad args → error message returned to agent.
6. Wrong namespace auto-correction.
7. Duplicate submit → idempotency guard.
8. Multiple proposals — submit one, leave other as draft.
9. LLM exposition JSON must not be parsed as a tool call.
10. Update then submit lifecycle.
11. Proposal ID prefix resolution.
12. Portal event emission: `ToolEvents` and `ThoughtEvents` are populated.

## System Fit
The primary regression suite for the agent's reasoning + proposal lifecycle. Each scenario maps to a field-observed or specification-required behaviour.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/proposal`
- `github.com/PixnBits/AegisClaw/internal/kernel`
