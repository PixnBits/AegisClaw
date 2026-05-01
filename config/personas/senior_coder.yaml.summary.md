# `config/personas/senior_coder.yaml` — Summary

## Purpose

Defines the **SeniorCoder** Governance Court persona. This reviewer acts as a principal software engineer evaluating practical code quality, correctness, and maintainability of a proposed skill.

## Key Fields

| Field | Value / Description |
|---|---|
| `name` | `SeniorCoder` |
| `role` | `code_quality` |
| `system_prompt` | Instructs a 15+ year experienced Go engineer to assess correctness, error handling completeness, performance, maintainability, concurrency primitive usage, resource cleanup, and API design |
| `models` | `mistral-nemo`, `llama3.2:3b` |
| `weight` | `0.25` — tied for highest weight with CISO |
| `output_schema` | `verdict`, `risk_score`, `evidence`, `questions`, `comments` |

## Code Quality Focus Areas

- Code correctness and logic errors
- Error handling completeness
- Performance implications
- Maintainability and readability
- Proper use of concurrency primitives (goroutines, channels, mutexes)
- Resource cleanup and leak prevention
- API design quality

## Fit in the Broader System

The SeniorCoder and CISO are the two highest-weighted personas (0.25 each), ensuring that both code quality and security carry equal influence in the consensus decision. Operates in `fallback` mode (tries `mistral-nemo` first, falls back to `llama3.2:3b`). Verdict is processed by `internal/court` alongside the other four persona verdicts.

## Notable Dependencies

- `config/personas.yaml` provides model routing and temperature (0.5).
- Part of the five-persona Governance Court ensemble defined in `internal/court`.
