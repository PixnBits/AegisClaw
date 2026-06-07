# AegisClaw Product Requirements Document

**Paranoid-by-design, self-evolving local agent platform**

## Overview

AegisClaw is a local-first AI agent platform that runs entirely on your hardware. Every component that touches untrusted input, LLM output, or generated code runs inside its own Firecracker microVM. The system is designed to be paranoid by default while remaining practical to use and extend.

## Core Documents

- ** (vision-and-goals.md)** — Why this project exists and what success looks like
- ** (user-personas.md)** — Who this platform is built for
- ** (collaboration-model.md)** — How users, Project Manager, Court personas, and SDLC specialists collaborate in channels with on-demand agents *(new — supersedes conversation-model.md)*
- ** (agent-autonomy.md)** — How agents earn increasing levels of trust
- ** (governance-court.md)** — The seven-persona review system *(updated for Agent-based Court in channels)*
- ** (sdlc-governance.md)** — How the Court controls every code change *(updated for SDLC Agents + PM coordination)*
- ** (runtime-architecture.md)** — The minimal daemon and microVM architecture *(updated with dynamic lifecycle)*
- ** (security-model.md)** — The overall security philosophy and guarantees
- ** (secret-management.md)** — How secrets are handled safely
- ** (skill-creation.md)** — How the system safely extends itself
- ** (glossary.md)** — Key terms and definitions *(new terms added)*

## Current Status

This PRD has been restructured and updated based on lessons learned from the first implementation. Each document is focused and scoped to fit comfortably in an LLM’s context window.

**June 2026 restructure note**: Major update to collaboration model (channels + role-based multi-agent + Project Manager + on-demand <1s lifecycle). See `collaboration-model.md` and `collaboration-restructure-plan.md` for details. Old `conversation-model.md` has been superseded and removed.

## Related Documents

- **[../architecture.md](../architecture.md)** — High-level system architecture and principles
- **[runtime-architecture.md](./runtime-architecture.md)** — Detailed runtime requirements (this index links to all PRD docs)
- **[../specs/](../specs/)** — Component-level specifications
- **[glossary.md](./glossary.md)** — All key term definitions
- **[collaboration-restructure-plan.md](./collaboration-restructure-plan.md)** — Implementation plan for the multi-agent channels restructure