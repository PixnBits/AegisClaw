# `config/templates/skill_codegen.yaml` — Summary

## Purpose

Defines the **`skill_codegen`** LLM prompt template used by the builder pipeline to generate a complete, production-ready Go skill implementation from a `SkillSpec`. This is the primary code-generation template — the first step in the builder pipeline before linting, security gates, and packaging.

## Key Fields

| Field | Description |
|---|---|
| `name` | `skill_codegen` |
| `description` | Generate a complete Go skill implementation from a SkillSpec |
| `system` | Expert Go developer system prompt; emphasises security, full error handling, vsock-based communication, read-only rootfs constraints, and JSON output format |
| `user` | Template accepting `{{skill_spec}}`; demands `main.go`, unit tests (> 80% coverage), `go.mod`, structured logging (`go.uber.org/zap`), and no stub code |

## Output Contract

The LLM must return a single JSON object:
```json
{
  "files": {"relative/path.go": "file content..."},
  "reasoning": "design decision explanation"
}
```
All file paths must be relative (no leading `/`).

## Security Constraints in the Prompt

- Validate all inputs; handle all errors
- No `unsafe` operations
- Skills run in Firecracker microVMs: vsock communication, read-only rootfs, writable `/workspace` only, network restricted to declared policy

## Fit in the Broader System

Used by `internal/builder` during the `codegen` pipeline step. The generated files are then passed through `skill_fix.yaml` if build errors occur, or `skill_edit.yaml` if Court feedback requires changes. The `skill_script_runner.yaml` template is used instead for skills that expose scripting runtimes.

## Notable Dependencies

- `go.uber.org/zap` — required logging library in generated code
- `internal/builder` — pipeline orchestrator that loads and renders this template
