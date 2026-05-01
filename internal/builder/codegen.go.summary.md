# `codegen.go` — Code Generation & Skill Spec

## Purpose
Defines the `SkillSpec` data model (the full specification for a skill), the `CodeGenerator` that sends generation requests to a builder sandbox via the kernel control plane, and the built-in `PromptTemplate` library used to construct structured LLM prompts.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `SkillSpec` | Complete skill definition: name, description, `[]ToolSpec`, `SkillNetworkPolicy`, secrets refs, persona requirements, language, entry point, dependencies, test requirements. |
| `ToolSpec` | A single tool/function: name, description, input/output schema. |
| `SkillNetworkPolicy` | Sandbox network rules: `DefaultDeny`, `AllowedHosts`, `AllowedPorts`, `AllowedProtocols`. |
| `SkillSpec.Validate()` | Enforces naming regex (`^[a-z][a-z0-9_-]{1,62}$`), description length, ≥1 tool, supported language, non-empty entry point, and `DefaultDeny == true`. |
| `CodeGenRequest` / `CodeGenResponse` | Input (spec, existing code, feedback, round, prompt, workspace context) and output (files map, reasoning, round, duration) for a single generation. |
| `PromptTemplate` | Name/description + system/user templates with `{{placeholder}}` substitution via `Format`. |
| `CodeGenerator` | Serialises a `CodeGenRequest`, sends `"codegen.generate"` to the builder sandbox, parses the response, and emits an audit log entry. |
| `DefaultTemplates()` | Returns four built-in templates: `skill_codegen`, `skill_edit`, `skill_fix`, `skill_script_runner`. |

## How It Fits Into the Broader System
`CodeGenerator` is called by `Pipeline.Execute` (round 1) and `IterationEngine.RunFixLoop` (rounds 2–4) to produce or refine skill source files. The `WorkspaceSkillContext` field lets users inject project-specific guidance without touching Court-reviewed templates.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel` — control plane + audit.
- `go.uber.org/zap`.
