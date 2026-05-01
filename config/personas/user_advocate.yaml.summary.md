# `config/personas/user_advocate.yaml` — Summary

## Purpose

Defines the **UserAdvocate** Governance Court persona. This reviewer evaluates a proposed skill from the perspective of the humans who will operate, maintain, and depend on the system — focusing on UX, documentation, backward compatibility, and operational burden.

## Key Fields

| Field | Value / Description |
|---|---|
| `name` | `UserAdvocate` |
| `role` | `usability` |
| `system_prompt` | Instructs the reviewer to assess user experience impact, error message clarity, documentation completeness, backward compatibility, configuration complexity, operational burden, and observability/debugging ease |
| `models` | `llama3.2:3b`, `mistral-nemo` |
| `weight` | `0.15` — equal to Tester, lightest in the court |
| `output_schema` | `verdict`, `risk_score`, `evidence`, `questions`, `comments` |

## Usability Focus Areas

- User experience impact of the change
- Error message clarity and actionability
- Documentation completeness
- Backward compatibility preservation
- Configuration complexity (simplicity wins)
- Operational burden on administrators
- Observability and debugging ease

## Fit in the Broader System

The UserAdvocate is unique among the five personas in that it prioritises `llama3.2:3b` over `mistral-nemo`, reflecting that usability assessments benefit from a conversational, accessible model. Temperature of 0.7 (the highest in the court) allows for more varied, exploratory reasoning about user needs. Together with the Tester, it brings non-security, non-code perspectives to the consensus.

## Notable Dependencies

- `config/personas.yaml` provides the full model routing and temperature.
- `internal/court` loads all five personas and aggregates their weighted verdicts.
