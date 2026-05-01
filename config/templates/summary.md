# `config/templates/` — Directory Summary

## Overview

The `config/templates/` directory contains YAML files that define LLM prompt templates and skill specifications used by the builder pipeline in `internal/builder`. Templates drive code generation, iterative editing, error fixing, semantic tool discovery, and hardened script-runner skill creation.

## Files

| File | Name | Type | Purpose |
|---|---|---|---|
| `skill_codegen.yaml` | `skill_codegen` | Prompt template | Initial full skill code generation from a `SkillSpec` |
| `skill_edit.yaml` | `skill_edit` | Prompt template | Targeted iterative edits based on Court feedback or requirement changes |
| `skill_fix.yaml` | `skill_fix` | Prompt template | Minimal error-only fixes for build/test/lint failures |
| `skill_lookup.yaml` | `lookup` | Skill spec | Semantic tool-discovery skill (vector-store backed, Gemma 4 tool blocks) |
| `skill_script_runner.yaml` | `skill_script_runner` | Prompt template | Hardened Go wrapper for Python/Node.js/Bash script execution |

## Common Template Structure

LLM prompt templates (`skill_codegen`, `skill_edit`, `skill_fix`, `skill_script_runner`) each define:
- **`name`** — identifier used by the builder to select the template
- **`description`** — human-readable summary
- **`system`** — LLM system prompt with security and quality constraints
- **`user`** — Go template string with `{{variable}}` placeholders
- **Output contract** — always a JSON object with `files` (map of relative path → content) and `reasoning`

## Fit in the Broader System

These templates are the LLM instruction layer of the build pipeline. `internal/builder` selects and renders them at each pipeline stage, passing the rendered prompt to the Ollama LLM via `internal/llm`. The pipeline typically flows: `skill_codegen` → (fix loop via `skill_fix`) → security gates → (edit loop via `skill_edit` if Court requests changes) → artifact packaging. `skill_lookup.yaml` is distinct — it is a skill spec that gets built and deployed, not a build-time prompt.

## Notable Dependencies

- `internal/builder` — pipeline orchestrator that loads, selects, and renders these templates
- `internal/llm` — LLM client that executes the rendered prompts against Ollama
- `go.uber.org/zap` — required in all generated skill code
