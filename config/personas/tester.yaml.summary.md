# `config/personas/tester.yaml` — Summary

## Purpose

Defines the **Tester** Governance Court persona. This reviewer acts as a QA engineer and testing specialist, evaluating test coverage, edge-case handling, regression risk, and overall testability of a proposed skill.

## Key Fields

| Field | Value / Description |
|---|---|
| `name` | `Tester` |
| `role` | `test_coverage` |
| `system_prompt` | Instructs a QA specialist to assess test coverage completeness, edge case handling, error path testing, integration test adequacy, regression risk, design testability, and mocking/isolation in tests |
| `models` | `llama3.2:3b` (single model) |
| `weight` | `0.15` — lightest weight alongside UserAdvocate |
| `output_schema` | `verdict`, `risk_score`, `evidence`, `questions`, `comments` |

## Testing Focus Areas

- Test coverage completeness (targeting > 80%)
- Edge case and boundary condition handling
- Error path and failure mode testing
- Integration test adequacy
- Regression risk assessment
- Testability and mockability of design

## Fit in the Broader System

The Tester persona uses only `llama3.2:3b` (no fallback), reflecting a lighter compute profile appropriate for its more focused role. Weight of 0.15 positions it as a secondary reviewer — its verdict is significant but less decisive than the CISO or SeniorCoder. Combined with the UserAdvocate's 0.15, the two lighter personas together contribute 30% of the total consensus score.

## Notable Dependencies

- `config/personas.yaml` provides temperature (0.6) and routing mode (fallback).
- Verdict consumed by `internal/court` consensus engine.
