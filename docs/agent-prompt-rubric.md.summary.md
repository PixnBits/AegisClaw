# `docs/agent-prompt-rubric.md` — Summary

## Purpose

An evaluation rubric for manually or semi-automatically grading AegisClaw's main-agent system prompt against a set of observable criteria. Designed to be run with any Ollama model, 3 times per test case to capture variation.

## Scoring Scale

- **0** — Fail: criterion clearly violated
- **1** — Partial: criterion partially met or inconsistent
- **2** — Pass: criterion fully satisfied

A prompt is considered "good" when it averages ≥ 1.5 on every criterion.

## Criteria (Partial List)

| Code | Criterion |
|---|---|
| C1 | **Conversational grounding** — greets user, responds naturally; does NOT call a tool for a simple "hello" |
| C2 | **Tool use only when warranted** — calls tools only when data or action is required; handles knowledge queries conversationally |
| (further) | Security invariants, ReAct format compliance, memory-first behaviour, task delegation, structured output, etc. |

## Fit in the Broader System

Used during prompt engineering and regression testing of the Orchestrator system prompt. Pairs with `docs/agent-prompts.md` (the actual prompt templates) and the golden trace tests in `cmd/aegisclaw/react_journey_test.go`. Supports the 98%+ ReAct format compliance target from the implementation plan.

## Notable Dependencies

- Ollama local inference (any model)
- Manual or automated test-case harness
- `docs/agent-prompts.md` — the prompts being evaluated
