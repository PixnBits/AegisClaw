# `config/personas/ciso.yaml` — Summary

## Purpose

Defines the **CISO** (Chief Information Security Officer) Governance Court persona. Provides the system prompt, model preferences, voting weight, and structured output schema used when this persona reviews a code proposal.

## Key Fields

| Field | Value / Description |
|---|---|
| `name` | `CISO` |
| `role` | `security` |
| `system_prompt` | Instructs the LLM to act as a CISO focused on data exposure, auth/authz flaws, cryptographic weaknesses, network attack surface, compliance, and supply-chain security |
| `models` | `mistral-nemo`, `llama3.2:3b` (preference order) |
| `weight` | `0.25` — contributes 25% of the weighted consensus score |
| `output_schema` | Requires `verdict` (approve/reject/ask/abstain), `risk_score` (float), `evidence` (list), `questions` (list), `comments` (string) |

## Security Focus Areas

- Data exposure and exfiltration risks
- Authentication and authorization flaws
- Cryptographic weaknesses
- Network attack surface expansion
- Compliance with security best practices
- Supply chain security

## Fit in the Broader System

Loaded by `internal/court` when instantiating the Governance Court. The CISO is the heaviest-weighted security reviewer (0.25) alongside SeniorCoder (0.25). Its verdict feeds into the weighted consensus algorithm that determines whether a proposal is approved, rejected, or escalated. Runs exclusively inside an isolated Firecracker microVM.

## Notable Dependencies

- `config/personas.yaml` overrides the model list and routing mode at runtime.
- Verdict JSON must match the `Verdict` struct in `internal/court`.
