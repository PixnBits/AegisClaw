# `config/templates/skill_script_runner.yaml` — Summary

## Purpose

Defines the **`skill_script_runner`** LLM prompt template used by the builder pipeline to generate a hardened Go wrapper skill that exposes script execution via approved interpreter runtimes. This template produces skills that allow the agent to run Python, Node.js, or Bash scripts safely inside the sandbox.

## Key Fields

| Field | Description |
|---|---|
| `name` | `skill_script_runner` |
| `description` | Generate a hardened Go wrapper skill for approved scripting runtimes |
| `system` | Expert Go security engineer prompt; mandates strict allowlist, length/size limits, context timeouts, `exec.CommandContext` (no shell interpolation), sensitive-value redaction, and stdout/stderr truncation |
| `user` | Template accepting `{{skill_spec}}`; requires vsock-based `main.go`, interpreter allowlist validation, timeout enforcement, output-size limits, and unit tests |

## Security Requirements in the Prompt

- Strict interpreter allowlist: `python3`, `node`, `bash` only when explicitly requested
- Script content and argument length limits
- Context timeout with forced process termination on expiry
- No shell interpolation for user-provided args — use `exec.CommandContext` directly
- Redact sensitive values from logs and error responses
- Return stdout/stderr with truncation limits

## Output Contract

Same as other builder templates: a JSON object with `files` (map of relative path → content) and `reasoning`.

## Fit in the Broader System

Used by `internal/builder` as a specialised alternative to `skill_codegen.yaml` when a proposal requests scripting runtime exposure. The generated skill runs inside a Firecracker microVM with the same isolation guarantees as any other skill. Enables the agent to execute shell automation, Python scripts, or Node.js code without broad privilege.

## Notable Dependencies

- `exec.CommandContext` from Go stdlib — required pattern for subprocess management
- `internal/builder` pipeline — selects this template based on proposal metadata
