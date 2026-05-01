# `docs/agentic-evolution.md` — Summary

## Purpose

Defines the architectural evolution of AegisClaw from a single ReAct Agent VM to a full **hierarchical multi-agent platform** with persistent memory, asynchronous signals/timers, human-in-the-loop approvals, and a local web dashboard — while strictly preserving paranoid isolation, Merkle auditing, and Governance Court oversight.

## Key Contents

### Vision & Motivation
- Single Agent VM + ReAct loop is a strong foundation but insufficient for long-horizon, proactive, scalable agency.
- Three core user stories driving the evolution: **Background Research**, **OSS Issue to PR**, **Recurring Summaries**.

### Hierarchical Architecture (Two-Tier)
- **Orchestrator** (persistent): long-lived agent VM; decomposes tasks; manages memory and timers; spawns Workers.
- **Workers** (ephemeral): specialised sub-agents (Researcher, Coder, Summarizer, Custom); created per task; snapshotted for fast restore.
- All communication via **AegisHub** (vsock, ACL-enforced).

### Memory System
- Tiered: working → episodic → semantic.
- Age-encrypted SQLite + vector store.
- PII redaction and compaction built in.

### Async Primitives
- Timer service (one-shot + cron), signal subscriptions, human approval queue.
- All state persisted to JSON and logged in Merkle audit tree.

### Success Metrics
- Zero isolation violations; full audit trail for async flows; 98%+ ReAct format compliance.

## Fit in the Broader System

North-star architecture document for Phases 1–5. Drives design of `internal/worker`, `internal/eventbus`, `internal/memory`, `internal/sessions`, and the `spawn_worker` / `set_timer` tools. Pairs with `docs/agent-prompts.md`, `docs/event-bus-and-async.md`, `docs/memory-store.md`.
