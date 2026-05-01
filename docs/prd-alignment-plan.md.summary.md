# `docs/prd-alignment-plan.md` — Summary

## Purpose

A concrete action plan derived from `docs/prd-deviations.md` for resolving every open gap between the implementation and the PRD/architecture specification. Tracks both already-resolved items and the remaining open deviations, providing implementation guidance for each.

## Key Contents

### Already Resolved (no further work needed)

| ID | Resolution |
|---|---|
| D1 | `FirecrackerLauncher` is the only court launcher; `DirectLauncher` deleted; daemon fails hard without KVM |
| D2-b | Daemon `makeChatMessageHandler` forwards to agent microVM; no direct Ollama calls from daemon |
| D2-c | `internal/court/direct_launcher.go` deleted; no opt-out from isolation anywhere |
| D3 | Court approval auto-triggers builder pipeline |
| D4 | Skill activation resolves artifact manifests from builder output |
| D10 | Versioned composition manifests with rollback implemented |
| D8 | SAST/SCA/secrets/policy-as-code security gates implemented |

### Open Items

- **D2-a**: Agent VM must run the full ReAct loop with AegisHub-routed `tool.exec` messages (not direct daemon calls). Currently the daemon drives the outer loop inline.
- **D2-c-cli**: CLI `ExecuteTool` callbacks still run on the host (should route via AegisHub).
- **DA**: IPC bus ACL enforcement not yet complete.
- **DB**: No central tool registry in daemon.

## Fit in the Broader System

Action companion to `docs/prd-deviations.md`. Consumed by developers picking up open deviation items. Every resolved item has a corresponding code change traceable through the git history and the Merkle audit log.
