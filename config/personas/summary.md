# `config/personas/` — Directory Summary

## Overview

The `config/personas/` directory contains one YAML file per Governance Court reviewer persona. Each file defines a distinct AI reviewer identity used by `internal/court` to evaluate code proposals before deployment. Together the five personas provide multi-perspective, weighted consensus review covering security, architecture, code quality, testing, and usability.

## Files

| File | Persona | Role | Weight | Primary Model |
|---|---|---|---|---|
| `ciso.yaml` | CISO | `security` | 0.25 | mistral-nemo |
| `security_architect.yaml` | SecurityArchitect | `architecture` | 0.20 | mistral-nemo |
| `senior_coder.yaml` | SeniorCoder | `code_quality` | 0.25 | mistral-nemo |
| `tester.yaml` | Tester | `test_coverage` | 0.15 | llama3.2:3b |
| `user_advocate.yaml` | UserAdvocate | `usability` | 0.15 | llama3.2:3b |

Weights sum to 1.0.

## Common Structure

Every persona YAML shares the same schema:
- **`name`** — identifier matched by `config/personas.yaml` routes
- **`role`** — semantic category for logging and reporting
- **`system_prompt`** — the full LLM system instruction injected at review time
- **`models`** — preferred Ollama model(s) in priority order
- **`weight`** — fractional contribution to weighted consensus (0–1)
- **`output_schema`** — required JSON verdict format (`verdict`, `risk_score`, `evidence`, `questions`, `comments`)

## Fit in the Broader System

These files are the identity layer of the Governance Court. `internal/court` loads them at startup, pairs each with a Firecracker reviewer microVM, and fans out proposal review tasks. The weighted consensus of all five verdicts determines whether a proposal is approved, rejected, or escalated to the operator. The runtime model routing (temperature, fallback/ensemble mode) is provided separately by `config/personas.yaml`.
