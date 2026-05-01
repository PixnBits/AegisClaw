# `docs/cli-design.md` — Summary

## Purpose

The full CLI interface specification for AegisClaw v2.0. Defines every top-level command, sub-command, flag, output format, confirmation behaviour, and safe-mode semantics. Status: ready for implementation; fully traceable to PRD §10, §13.2, and §13.3.

## Key Philosophy

- **Chat is the primary daily interface** — most operations are doable via natural language in `aegisclaw chat`.
- **Safe Mode** — strict minimal recovery environment with no LLM, no Court, no skills running.
- Secrets are **never** accepted or displayed via chat — only via dedicated CLI commands.
- High-risk actions always require explicit confirmation unless `--force` is used (with full audit trail).
- Everything is git-backed and recorded in the append-only Merkle-tree audit log.

## Top-Level Commands

| Command | Description |
|---|---|
| `aegisclaw init` | One-time setup (directory structure, keypair, audit log) |
| `aegisclaw start` | Start the coordinator daemon |
| `aegisclaw stop` | Gracefully stop the daemon |
| `aegisclaw status` | Show system status and health |
| `aegisclaw chat` | Interactive ReAct chat (primary interface) |
| `aegisclaw skill` | Manage skills (add, list, revoke, info, sbom) |
| `aegisclaw audit` | Query the append-only audit log |
| `aegisclaw secrets` | Manage secrets (add, list, rotate) — never exposes values |
| `aegisclaw self` | Self-improvement and system management |
| `aegisclaw version` | Show version and build info |

**Global Flags**: `--json`, `--verbose/-v`, `--dry-run`, `--force`

## Fit in the Broader System

Implemented in `cmd/aegisclaw`. The CLI communicates with the daemon via the Unix socket API (`internal/api`). Commands that require agent reasoning (e.g., `skill add` with natural language description) forward to the agent VM via AegisHub.
