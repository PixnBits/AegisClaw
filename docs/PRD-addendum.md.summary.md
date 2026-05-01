# `docs/PRD-addendum.md` — Summary

## Purpose

Defines additional product requirements for integrating OpenClaw-inspired usability, multi-channel support, and agent flexibility into AegisClaw — while strictly preserving (and strengthening) the paranoid-by-design isolation and governance model. Version 1.1 (April 2026) with a key clarification that Docker sandbox migration is deferred; Firecracker remains the only supported isolation backend.

## Key Sections

- **Background**: AegisClaw (security) + OpenClaw (usability) comparative analysis; Firecracker-only decision.
- **High-Level Objectives**: Feature parity with OpenClaw usability; sandboxed script runner as execution backbone; declarative workspace prompt injection; full auditability of new features.
- **Functional Requirements** (8+ areas):
  - Workspace and prompt-driven agent configuration (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`)
  - Multi-agent and session routing
  - Multi-channel gateway (Discord, Telegram, Slack, webhooks)
  - Sandboxed script runner (Python/Node.js/Bash with security guardrails)
  - Voice interaction (host-audio proxy, TTS model)
  - ClawHub registry bridge
  - Canvas-like rich UI
  - Self-improvement and governance integration

## Key Constraint

All new features must run inside Firecracker sandboxes, declare explicit capabilities, pass Governance Court review + SAST/SCA/secrets security gates, and contribute to the Merkle-tree audit log. No cloud dependency.

## Fit in the Broader System

Companion to `docs/PRD.md` (core requirements) and `docs/architecture-addendum.md` (architectural design). Drives Phase 1–3 work in `docs/implementation-plan-openclaw-integration.md`. Influences `internal/gateway`, `internal/worker`, `internal/workspace`, and `config/templates/skill_script_runner.yaml`.
