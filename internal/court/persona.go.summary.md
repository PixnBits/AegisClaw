# `persona.go` — Reviewer Persona Definition

## Purpose
Defines the `Persona` struct that describes a court reviewer identity, including its system prompt, preferred LLM models, voting weight, and output schema. Also implements field-level validation.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Persona` | Struct with fields: `Name`, `Role`, `SystemPrompt`, `Models []string`, `Weight float64`, `OutputSchema` |
| `Persona.Validate()` | Returns an error if any required field is missing or `Weight` is outside `(0, 1]` |

## Persona Fields
- **Name** — unique identifier used in consensus heatmaps and audit logs
- **Role** — human-readable role label (e.g., "security-reviewer")
- **SystemPrompt** — injected verbatim into the reviewer LLM context
- **Models** — ordered list of Ollama model names; first available is used
- **Weight** — influence on the weighted consensus vote (0 < weight ≤ 1)
- **OutputSchema** — optional JSON schema string for structured output enforcement

## Role in the System
The foundational data type for the court subsystem. `LoadPersonas` populates slices of `*Persona`; `EvaluateConsensus` reads `Persona.Weight`; `FirecrackerLauncher` and `InProcessSandboxLauncher` pass the persona's system prompt and model preferences to the LLM.

## Notable Dependencies
- Standard library only (`fmt`)
