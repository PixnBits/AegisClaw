# Vision & Goals

**Paranoid-by-design, self-evolving local agent platform**

## Vision

AegisClaw is a local-first AI agent platform that lets you safely extend an agent with new capabilities — without ever having to trust the new code.

You describe what you want in plain English, and the system proposes a new "skill", submits it to a **Governance Court** of five isolated AI reviewers, runs mandatory security gates, and deploys the approved code inside a Firecracker microVM. Every component boundary is a security boundary. Your API keys never touch prompts, logs, or LLM context.

The end result is an agent you can trust like a paranoid enterprise security team — but running entirely on your own hardware.

## Core Philosophy

- **Security is structural, not procedural** — Malicious or buggy skills are made *structurally impossible* through isolation, not just careful review.
- **Local-first by default** — Everything runs with Ollama. Any cloud LLM usage requires explicit user approval and Court review.
- **Trust is earned, never assumed** — Agents start with zero autonomy and must prove themselves through the Governance Court to gain more freedom.
- **Self-improving but human-supervised** — The system can propose improvements to itself, but every change must pass the Court and receive final human sign-off.
- **Everything is auditable and reversible** — An append-only Merkle-tree audit log records every action with cryptographic signatures.

## Key Goals

- Zero isolation violations in public red-team exercises by v1.0
- Adding a new skill takes less than 15 minutes of user time
- The system successfully proposes and merges at least one self-improvement within the first month of use
- Support at least 10 high-quality, production-ready skills with full audit trails
- New users can install and create their first skill in under 10 minutes
- Enterprise users can customize reviewer personas and policies while the core isolation guarantees remain immutable
