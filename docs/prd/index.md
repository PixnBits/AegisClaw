# AegisClaw Product Requirements Document

**Paranoid-by-design, self-evolving local agent platform**

## Overview

AegisClaw is a local-first AI agent platform that runs entirely on your hardware. Every component that touches untrusted input, LLM output, or generated code runs inside its own Firecracker microVM. The system is designed to be paranoid by default while remaining practical to use and extend.

## Core Documents

- ** (vision-and-goals.md)** — Why this project exists and what success looks like
- ** (user-personas.md)** — Who this platform is built for
- ** (conversation-model.md)** — How users and agents communicate
- ** (agent-autonomy.md)** — How agents earn increasing levels of trust
- ** (governance-court.md)** — The five-persona review system
- ** (sdlc-governance.md)** — How the Court controls every code change
- ** (runtime-architecture.md)** — The minimal daemon and microVM architecture
- ** (security-model.md)** — The overall security philosophy and guarantees
- ** (secret-management.md)** — How secrets are handled safely
- ** (skill-creation.md)** — How the system safely extends itself
- ** (glossary.md)** — Key terms and definitions

## Current Status

This PRD has been restructured and updated based on lessons learned from the first implementation. Each document is focused and scoped to fit comfortably in an LLM’s context window.

## Related Documents

- **[../architecture.md](../architecture.md)** — High-level system architecture and principles
- **[runtime-architecture.md](./runtime-architecture.md)** — Detailed runtime requirements (this index links to all PRD docs)
- **[../specs/](../specs/)** — Component-level specifications
- **[glossary.md](./glossary.md)** — All key term definitions