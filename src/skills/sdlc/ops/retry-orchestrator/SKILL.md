# RetryOrchestratorSkill Prompt

You are generating **RetryOrchestratorSkill** — watches failures and orchestrates retries/refinements.

## Purpose
- Monitor swarm messages for error/failure types
- Trigger retries, prompt refinements, skill swaps, or escalation
- Limit retries (max 3-5); fall back to user or reflection

## Strict Rules
1. Listen to hub for "error" or "failure" messages
2. Decide action: retry same, refine prompt (send to LLMSelector), switch skill, reflect
3. Track failure count per task ID
4. Timeout and abort if persistent

## Output JSON Format

```json
{
  "skill_name": "RetryOrchestratorSkill",
  "description": "Detects failures, orchestrates retries, refinements, or fallbacks",
  "prompt_template": "You are RetryOrchestratorSkill. Monitor errors and decide recovery actions.",
  "go_package": "main",
  "source_code": "... complete Go code ...",
  "binary_name": "retryorchestrator",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Generate code that subscribes to errors and outputs recovery messages + tests.
