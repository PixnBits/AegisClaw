# `docs/prd-deviations.md` — Summary

## Purpose

A living document that compares the current implementation against `docs/PRD.md` and `docs/cli-design.md`, tracking every known deviation between the north-star architecture and the shipped code. Updated across multiple dates in March–April 2026 as deviations are resolved or new ones are discovered.

## Key Contents

### Methodology
Code paths wired into the runnable product are treated as authoritative over package-level scaffolding. Uses repository code (not README intent) to determine what is actually implemented.

### Deviation Table (Selected Entries)

| ID | Status | Description |
|---|---|---|
| D1 | Resolved | Court reviewers run in Firecracker microVMs only; `DirectLauncher` deleted |
| D2 | Partially resolved | Main agent sandbox: daemon forwards to agent VM (D2-b resolved); full ReAct loop in VM (D2-a open); CLI tool callbacks on host (D2-c-cli open) |
| D2-a | Open | Agent VM must run full ReAct loop with AegisHub-routed `tool.exec` messages |
| D2-b | Resolved | Daemon now forwards chat messages to agent microVM |
| D2-c | Resolved | `DirectLauncher` deleted; no opt-out from microVM isolation |
| D3 | Resolved | Court approval auto-triggers builder pipeline |
| D8 | Resolved | SAST/SCA/secrets/policy-as-code gates implemented |
| D10 | Resolved | Versioned compositions with rollback |
| DA | Open | IPC bus ACL enforcement incomplete |
| DB | Open | No central tool registry in daemon |
| DC | Resolved | Lazy agent VM startup via `ensureAgentVM` |

## Fit in the Broader System

The ground truth for what is actually vs. aspirationally implemented. Pairs with `docs/prd-alignment-plan.md` (action items) and `docs/architecture.md` (north-star target).
