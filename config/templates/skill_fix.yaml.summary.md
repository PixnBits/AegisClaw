# `config/templates/skill_fix.yaml` — Summary

## Purpose

Defines the **`skill_fix`** LLM prompt template used by the builder pipeline to correct build errors, test failures, or lint issues in generated skill code. It is the targeted error-remediation template and enforces a minimal-change discipline.

## Key Fields

| Field | Description |
|---|---|
| `name` | `skill_fix` |
| `description` | Fix code based on build errors, test failures, or lint issues |
| `system` | Instructs the LLM to fix ONLY the reported errors — no refactoring or restructuring; maintain all existing functionality and tests |
| `user` | Template accepting `{{skill_spec}}`, `{{existing_code}}`, and `{{errors}}`; requires ALL files in the response |

## Template Variables

| Variable | Source |
|---|---|
| `{{skill_spec}}` | Original `SkillSpec` from the proposal |
| `{{existing_code}}` | Current file map from the previous pipeline pass |
| `{{errors}}` | Build output, test failure messages, or lint diagnostics |

## Output Contract

Returns a JSON object with all corrected files and a `reasoning` field that explains each fix applied. Requiring all files (not just changed ones) prevents partial state in the builder's file store.

## Discipline Enforced

- Fix only the reported errors
- Do not refactor or restructure unrelated code
- Preserve all existing tests
- Maintain complete output (all files, not just changed ones)

## Fit in the Broader System

Invoked by `internal/builder` in an automated fix loop after the compile, test, or lint step fails. Typically runs up to a configurable maximum retry count before the build is marked failed. Works in conjunction with `skill_codegen.yaml` (initial generation) and `skill_edit.yaml` (feedback-driven changes).

## Notable Dependencies

- `internal/builder` pipeline — drives the error-fix retry loop
- Build output from the sandboxed Firecracker builder microVM supplies `{{errors}}`
