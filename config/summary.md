# `config/` — Directory Summary

## Overview

The `config/` directory contains all static configuration and data files that define the identity, behaviour, and LLM prompt templates of the AegisClaw Governance Court and builder pipeline. No application code lives here — this is pure configuration consumed at daemon startup and during skill build operations.

## Structure

```
config/
├── personas.yaml              # LLM routing config for all five Court personas
├── personas/
│   ├── ciso.yaml              # CISO reviewer: security focus, weight 0.25
│   ├── security_architect.yaml # SecurityArchitect: architecture focus, weight 0.20
│   ├── senior_coder.yaml      # SeniorCoder: code quality focus, weight 0.25
│   ├── tester.yaml            # Tester: test coverage focus, weight 0.15
│   └── user_advocate.yaml     # UserAdvocate: usability focus, weight 0.15
└── templates/
    ├── skill_codegen.yaml     # Initial skill code generation prompt
    ├── skill_edit.yaml        # Iterative skill editing prompt
    ├── skill_fix.yaml         # Build/test/lint error correction prompt
    ├── skill_lookup.yaml      # Lookup skill spec (semantic tool discovery)
    └── skill_script_runner.yaml # Hardened script-runner skill prompt
```

## Sub-directory Roles

### `personas.yaml`
Central routing table that maps each persona name to Ollama model(s), inference temperature, routing mode (`ensemble` / `fallback`), token budget, and the shared verdict JSON schema. Runtime settings here override the model preference lists in individual persona files.

### `personas/`
Five YAML files, one per Governance Court reviewer. Each file defines the persona's name, role, LLM system prompt, model preferences, voting weight (weights sum to 1.0), and structured output schema. Loaded by `internal/court` when instantiating reviewer microVMs.

### `templates/`
Four LLM prompt templates (`skill_codegen`, `skill_edit`, `skill_fix`, `skill_script_runner`) and one skill specification (`skill_lookup`). Loaded by `internal/builder` during the skill build pipeline. Templates use Go `{{variable}}` syntax for dynamic content injection.

## Fit in the Broader System

- **`internal/court`** loads persona definitions to configure each of the five isolated reviewer Firecracker microVMs.
- **`internal/llm`** uses `personas.yaml` routing to select the correct Ollama model and inference settings per reviewer.
- **`internal/builder`** loads templates from `config/templates/` at each build pipeline stage to drive LLM-based code generation and repair.

## Key Design Decisions

- All Court weights sum to exactly 1.0 (CISO 0.25 + SeniorCoder 0.25 + SecurityArchitect 0.20 + Tester 0.15 + UserAdvocate 0.15).
- Every LLM interaction produces structured JSON output — no free-form text parsing required.
- The CISO uses `ensemble` mode (both models queried, results aggregated) for maximum rigour; all others use `fallback`.
