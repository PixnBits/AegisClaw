# `docs/` — Directory Summary

## Overview

The `docs/` directory contains AegisClaw's living specification suite: product requirements, architecture model, implementation plans, security analysis, CLI specification, developer tutorials, and per-feature design documents. All documents are Markdown except one JSON schema. Together they serve as the north-star reference for implementation decisions.

## Files

| File | Type | Purpose |
|---|---|---|
| `PRD.md` | Core spec | Full product requirements document (v2.0): vision, security principles, Governance Court, CLI requirements, phased roadmap |
| `architecture.md` | Core spec | Authoritative component interaction model; all-in-microVM security rule; component map with AegisHub as sole IPC router |
| `cli-design.md` | Core spec | Full CLI interface specification (every command, flag, output format, safe mode) |
| `threat-model.md` | Security | Assets, trust boundaries, threat actors, attack vectors, and mitigations |
| `first-skill-tutorial.md` | Tutorial | Step-by-step guide: proposal → Court review → builder pipeline → activation → invocation |
| `agent-prompts.md` | Design | Centralised LLM system prompts and few-shot examples for Orchestrator and Workers |
| `agent-prompt-rubric.md` | Tooling | Evaluation rubric (0–2 scale) for grading the main agent system prompt |
| `agentic-evolution.md` | Design | Hierarchical multi-agent platform vision: persistent Orchestrator + ephemeral Workers, memory, async |
| `event-bus-and-async.md` | Design | Event Bus, Timer Service, Signal Router, Wakeup Dispatcher specification |
| `memory-store.md` | Design | Tiered memory architecture: age-encrypted storage, PII scrubbing, compaction, access proxy |
| `web-dashboard.md` | Design | Local web portal spec: pages, technology (pure Go templates + SSE), zero-cloud principle |
| `network-secrets-spec.md` | Design | FQDN-based egress control and automated secret injection at activation |
| `PRD-addendum.md` | Requirements | OpenClaw-inspired usability requirements: workspace injection, multi-channel gateway, script runner |
| `architecture-addendum.md` | Architecture | Hybrid three-layer target architecture integrating OpenClaw usability layers |
| `implementation-plan.md` | Roadmap | Phased delivery plan for Phases 0–5 (all complete); acceptance criteria per phase |
| `implementation-plan-openclaw-integration.md` | Roadmap | Phased plan for OpenClaw-inspired feature integration |
| `prd-deviations.md` | Tracking | Known gaps between north-star spec and shipped code; deviation resolution status table |
| `prd-alignment-plan.md` | Tracking | Action plan for resolving open deviations |
| `sdlc-visibility-implementation.md` | Tracking | Portal SDLC visibility feature implementation notes; template fragment fix |
| `pr19-network-secrets-security-ux-plan.md` | Plan | PR 19 hardening: agent knowledge improvements + secret CLI write-only interface |
| `CHANGELOG.md` | History | Per-phase changelog: new packages, config, CLI, behaviour |
| `schemas/proposal-create-draft.schema.json` | Schema | JSON Schema for `proposal.create_draft` tool input validation |

## Key Relationships

- **`PRD.md` ← `prd-deviations.md` ← `prd-alignment-plan.md`** — requirements → gaps → action items
- **`architecture.md` ← `architecture-addendum.md`** — core model → hybrid extension
- **`agentic-evolution.md` → `agent-prompts.md`, `event-bus-and-async.md`, `memory-store.md`, `implementation-plan.md`** — vision → specifications → delivery plan
- **`first-skill-tutorial.md`** — the single most useful onboarding document; exercises the entire system end-to-end
