# `codegen_test.go` — Code Generator Tests

## Purpose
Unit tests for `SkillSpec` validation, `CodeGenRequest`/`CodeGenResponse` validation, `PromptTemplate.Format`, and `DefaultTemplates`. All tests are pure data logic and do not require a running builder sandbox.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestSkillSpecValidation` | Table-driven: empty name, invalid name pattern, empty description, oversized description, no tools, unsupported language, missing entry point, `DefaultDeny == false`, invalid secret ref each return the correct error. |
| `TestCodeGenRequestValidation` | Invalid `SkillSpec`, out-of-range `Round` (0 / 4), missing `SystemPrompt`, and default `MaxTokens` fallback. |
| `TestCodeGenResponseValidation` | No files, empty path, path traversal (`../evil`), empty content all fail; valid response passes. |
| `TestPromptTemplateFormat` | Single and multiple `{{key}}` substitutions; missing key leaves placeholder unchanged. |
| `TestDefaultTemplates` | Asserts all four built-in templates are present and have non-empty `System` and `User` fields. |
| `TestNewCodeGeneratorValidation` | Nil builder runtime, nil kernel, or empty templates map each return errors. |
| `TestSkillNetworkPolicy` | `DefaultDeny == false` is rejected by `SkillSpec.Validate()`. |

## How It Fits Into the Broader System
These tests lock down the input-validation contract that protects the builder sandbox and LLM prompt assembly from malformed or adversarial skill specifications.

## Notable Dependencies
- Standard library `strings`, `testing`.
