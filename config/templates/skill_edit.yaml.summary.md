# `config/templates/skill_edit.yaml` — Summary

## Purpose

Defines the **`skill_edit`** LLM prompt template used by the builder pipeline to apply targeted modifications to an existing skill based on Governance Court feedback or changed requirements. It is the iterative refinement template used after an initial code generation round.

## Key Fields

| Field | Description |
|---|---|
| `name` | `skill_edit` |
| `description` | Edit an existing skill based on feedback or requirement changes |
| `system` | Instructs the LLM to apply minimal, targeted changes; never remove existing functionality; preserve all existing tests and add new ones for changed behaviour |
| `user` | Template accepting `{{skill_spec}}`, `{{existing_code}}`, and `{{feedback}}`; requires ALL files (unchanged + changed) in the response |

## Template Variables

| Variable | Source |
|---|---|
| `{{skill_spec}}` | Original `SkillSpec` from the proposal |
| `{{existing_code}}` | Map of filename → content from the previous codegen pass |
| `{{feedback}}` | Court review feedback, operator comments, or error descriptions |

## Output Contract

The LLM must return a JSON object containing **all** files (not just changed ones), plus a reasoning string explaining what was changed and why. This ensures the builder always has a complete, self-contained file set.

## Fit in the Broader System

Invoked by `internal/builder` when a skill needs iterative improvement: after Court review requests changes (`ask` verdict), after the operator provides manual feedback, or during multi-round refinement cycles. Complementary to `skill_fix.yaml` (build errors) and `skill_codegen.yaml` (initial generation).

## Notable Dependencies

- `internal/builder` pipeline — renders and invokes this template
- Governance Court verdicts from `internal/court` supply the `{{feedback}}` variable
