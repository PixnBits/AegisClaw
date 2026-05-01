# `docs/implementation-plan-openclaw-integration.md` — Summary

## Purpose

A phased implementation plan for integrating OpenClaw-inspired usability features into AegisClaw. Covers workspace prompt injection, multi-channel gateway, session routing, sandboxed script runner, voice interaction, ClawHub registry bridge, and Canvas-like UI — all while keeping Firecracker as the only supported isolation backend.

## Key Decisions

- Docker sandbox migration: **deferred indefinitely** (not available on target Linux environment).
- Firecracker microVMs are the required and only supported isolation backend.
- All new features must run inside sandboxes, declare capabilities, pass Governance Court + security gates, and log to the Merkle audit tree.

## Phased Delivery (High Level)

| Phase | Focus |
|---|---|
| 1 | Workspace prompt injection (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`); multi-agent session routing |
| 2 | Multi-channel gateway (Discord, Telegram, Slack, webhooks) as governed skills |
| 3 | Sandboxed script runner; voice interaction adapter; ClawHub registry bridge |
| 4 | Canvas-like rich UI; advanced self-improvement proposals |

Each phase: deliverables, acceptance criteria, security review requirements.

## Foundation to Leverage

The hardened script runner (`config/templates/skill_script_runner.yaml`), guest-agent, Governance Court, builder pipeline, and Merkle audit log are explicitly identified as the stable foundation.

## Fit in the Broader System

Companion to `docs/PRD-addendum.md` (requirements) and `docs/architecture-addendum.md` (architecture). Deliverables map to `internal/workspace`, `internal/gateway`, `internal/registry`, and `internal/worker`.
