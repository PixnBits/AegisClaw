# `config/personas/security_architect.yaml` — Summary

## Purpose

Defines the **SecurityArchitect** Governance Court persona. Provides the system prompt, model preferences, voting weight, and structured output schema used when this persona evaluates the architectural fitness and isolation properties of a code proposal.

## Key Fields

| Field | Value / Description |
|---|---|
| `name` | `SecurityArchitect` |
| `role` | `architecture` |
| `system_prompt` | Instructs the LLM to evaluate isolation boundary integrity, privilege escalation vectors, trust boundary violations, defence in depth, least privilege, secure defaults, and attack surface minimisation |
| `models` | `mistral-nemo`, `llama3.2:3b` |
| `weight` | `0.2` — 20% of weighted consensus |
| `output_schema` | `verdict`, `risk_score`, `evidence`, `questions`, `comments` |

## Architectural Focus Areas

- Isolation boundary integrity (sandbox escape, microVM containment)
- Privilege escalation vectors
- Trust boundary violations
- Defence in depth adherence
- Principle of least privilege
- Secure defaults and attack surface minimisation

## Fit in the Broader System

Complements the CISO persona (which focuses on data/auth/compliance) with a structural, architecture-level view. Weighted at 0.2 — slightly lighter than the CISO and SeniorCoder reviewers — reflecting its more specialised scope. Verdict contributes to the same weighted consensus pipeline in `internal/court`. Runs inside an isolated Firecracker microVM.

## Notable Dependencies

- `config/personas.yaml` supplies the active model list and routing mode.
- Verdict JSON parsed by `internal/court` consensus logic.
