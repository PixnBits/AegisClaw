# `docs/web-dashboard.md` — Summary

## Purpose

Specifies the **local web dashboard** — AegisClaw's browser-based control plane for monitoring and managing the hierarchical multi-agent system. 100% local, no cloud or telemetry. Serves as the single pane of glass for paranoid operators who want full visibility without sacrificing security.

## Key Contents

### Vision
Zero-trust; all data from audited sources (Merkle tree, Event Bus, Memory Store). Full transparency over every action, timer, memory entry, and agent step. Responsive and lightweight (32 GB machine + GPU inference).

### Core Pages

| Page | Description |
|---|---|
| Overview / Home | System health, recent activity timeline, quick stats (pending approvals, active timers, memory store size), global search |
| Live Agents | Running microVMs (Orchestrator + Workers); real-time resource usage; current ReAct step; pause/snapshot/terminate controls |
| Async Hub | Active timers and pending signals; cancel/inspect; human approval queue |
| Memory Vault | Browse and search memory entries; TTL info; right-to-forget deletion |
| Audit Explorer | Merkle tree viewer; verify chain integrity; trace individual events |
| Skills & Proposals | Active skills; proposal status (draft → Court → approved → deployed) |
| Settings | Config editor (Court strictness, memory compaction, notification preferences) |
| Live Chat | In-browser chat interface with real-time tool-call visibility (streamed SSE) |

### Technology
Pure Go `html/template`; SSE for live updates; no external UI frameworks; HTMX-free.

## Fit in the Broader System

Implemented as `internal/dashboard`, served by the portal microVM (`cmd/aegisportal`) at `http://127.0.0.1:7878`. In-process dispatch via `dashboard.APIClient` avoids a socket round-trip. Specified by `docs/web-dashboard.md`.
