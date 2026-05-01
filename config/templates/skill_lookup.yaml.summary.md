# `config/templates/skill_lookup.yaml` — Summary

## Purpose

Defines the **`lookup`** skill specification — the semantic tool-discovery skill that keeps the main agent's context window small by retrieving only the most relevant 4–6 tools at runtime from a vector store. This file is a hybrid: a `SkillSpec` YAML (not an LLM prompt template) that describes the lookup skill's interfaces, resources, and implementation notes for the builder pipeline.

## Key Fields

| Field | Description |
|---|---|
| `name` | `lookup` |
| `type` | `skill` (Go skill, not a prompt template) |
| `version` | `0.1.0` |
| `interfaces` | `LookupTools` (similarity search returning Gemma 4 `<\|tool\|>` blocks) and `IndexTool` (index/re-index a single tool entry) |
| `resources` | `persistent_volume` at `/data/vectordb`; embedding model `all-minilm-l6-v2` (90 MB) |
| `dependencies` | `chromem-go` (in-process vector DB), `onnxruntime-go` (planned), `all-minilm-l6-v2-go` (planned) |
| `priority` | `high` |
| `governance_review` | `required` |

## Interfaces

- **`LookupTools`** — accepts `query` (string) + `max_results` (int, default 6); returns pre-formatted Gemma 4 tool blocks.
- **`IndexTool`** — accepts `name`, `description`, `skill_name`, `parameters`; returns `indexed_count`.

## Implementation Notes

- Embeddings use a deterministic pure-Go FNV-32 hash (384 dims, L2-normalised) as a stand-in for all-MiniLM-L6-v2.
- Built-in daemon tools are seeded at startup via `seedLookupStore`.
- Every new skill created by the builder is automatically indexed.
- No external network access — internal AegisHub only.

## Fit in the Broader System

This skill is core infrastructure for the ReAct agent loop. By returning only the most relevant tools, it prevents context-window overflow and focuses the agent's reasoning. Referenced by `internal/lookup`.
