# Permissions Model Specification

**Status:** Draft — implementation target on `feat/permissions-model` branch
**Last Updated:** 2026-06-20

## Purpose

Define a fine-grained, user-controlled, auditable capability-grant system so every agent and microVM can only discover and invoke the exact tools/commands it has been authorized for. This is the technical realization of the Android-style permissions model described in the companion PRD.

## Responsibility Boundaries

**Store VM owns (source of truth):**
- Durable, signed storage of all grants (alongside autonomy grants and timers; tamper-evident Merkle audit log)
- New commands: `permission.grant`, `permission.revoke`, `permission.list`, `permission.check`, `permission.snapshot` (for a subject), `permission.request` (event for CISO/user review)
- Grant/revoke authorization checks (user via Portal, CISO after opt-in, or Court for high-impact)
- Publishing of `permission.granted` / `permission.revoked` / `permission.denied` events via Hub
- Reconciliation and audit of denied attempts

**AegisHub owns:**
- Extension of ACL enforcement to include fine-grained permission checks for sensitive commands
- Distribution of signed permission snapshots to microVMs during/after registration handshake
- Hot-reload / EventBus-driven invalidation when grants change (so running agents refresh their local view)
- Rejection with clear `ERR_PERMISSION_DENIED` for unauthorized cross-VM attempts

**Agent Runtime (and persona microVMs: project-manager*, coder*, court-persona-*, etc.) owns:**
- Local filtering of `AgentSkillIndex` (or equivalent tool registry) using the permission snapshot received from Hub
- Runtime enforcement before any tool/skill invocation (even "local" skills)
- Emitting `permission.request` events when it hits an authorization boundary (with context for review)
- Clear error surfaces so the 6-step loop and user see exactly why a tool was unavailable

**Web Portal / CLI owns:**
- The primary grant/revoke UI (initial implementation)
- Display of active subjects, their current grants, pending requests, and the global tool registry
- Explicit opt-in toggle for CISO delegation (stored in user preferences / Store)

**CISO Persona (post explicit user opt-in only):**
- Receives `permission.request` events via channel or dedicated topic
- Evaluates requests using its own limited tool set + Court review path for high-impact items
- Proposes grants via `permission.grant` (still auditable and revocable by user)
- Never auto-grants; delegation can be revoked instantly by user

## Core Model

- **Capability** = any `noun.verb` command or registered skill tool (examples: `channel.create`, `channel.post`, `proposal.submit`, `store.create_channel`, `discord_monitor.send_message`, scoped `llm.*`).
- **Subject** = microVM instance ID or persona-type pattern (`project-manager*`, `coder-xyz`).
- **Grant** = allow record: `{subject, capability, granted_by, granted_at, reason?, conditions?}`. Stored durably and versioned in Store.
- **Default policy**: Deny everything except explicit grants present in `config/acls.yaml` bootstrap + minimal initial Store state required for core daemon/portal flows.
- Grants are **additive** to (never replace) the existing ACL layer. ACLs remain the coarse inter-VM routing gate; permissions add the fine-grained "may this subject actually use this specific capability?" check.

## New / Extended Commands

All routed through AegisHub with normal signature + ACL validation, then handled by Store.

- `permission.grant` — subject, capability, [reason]
- `permission.revoke` — subject, capability, [reason]
- `permission.list` — optional filters (subject, capability prefix)
- `permission.check` — subject, capability → {allowed: bool, grant_details?, reason?}
- `permission.snapshot` — subject → full current allow-set (used to initialize or refresh an agent's local index)
- Events (published by Store via Hub): `permission.granted`, `permission.revoked`, `permission.denied` (with full context for audit + CISO)

High-impact grants (anything that can create persistent state, contact external services, or affect other agents) can be configured to require Court review before Store accepts them.

## Enforcement Flow (End-to-End)

1. MicroVM starts / reconnects → AegisHub completes signed handshake → Hub calls `permission.snapshot` on Store for the subject (or persona pattern) → Hub pushes the signed snapshot to the microVM.
2. Agent Runtime (or project-manager, court-persona, etc.) receives snapshot and filters its local `AgentSkillIndex` (or equivalent) so that `ListSkills`, `SearchTools`, `HandleToolCommand("tool.list"/"tool.search")`, and prompt injection (`FormatAvailableTools`) only ever surface permitted items.
3. During 6-step loop or direct tool invocation: the act/execute layer performs a final `permission.check` (or relies on the already-filtered index). Unpermitted calls are rejected with `ERR_PERMISSION_DENIED`, logged, and optionally emit a `permission.request` event.
4. Grant change (user toggle, CISO proposal, or Court decision) → Store records it → publishes event → Hub pushes incremental update or invalidation to affected live microVMs → local indexes refresh.
5. All denied attempts and grant mutations are appended to the Store audit log (Merkle chain, queryable via existing `audit.*` commands).

## Integration with Existing Components

- **AgentSkillIndex** (`internal/agent/skills/index.go`): Primary local enforcement surface. Extend `NewAgentSkillIndex` / init path and `SearchTools` to accept (and persistently filter by) a permission allow-set. The existing Jaccard/substring/Levenshtein scoring continues to work on the reduced set. `HandleToolCommand` already dispatches `tool.list`/`tool.search` — they automatically become permission-aware.
- **AegisHub + ACLs** (`config/acls.yaml`, `cmd/aegishub`): ACL rules stay as the first gate (e.g. `agent*` may talk to `store` for `channel.*`). Permissions add the second, finer gate inside those flows. Minimal new ACL entries will be needed for the new `permission.*` commands and snapshot distribution.
- **Store VM** (`cmd/store/main.go`, `docs/specs/store-vm.md`): Add grant storage (re-use or extend the grants/timers JSON + SQLite tables), implement the new commands, event publishing, and authorization logic for who may grant what. Build directly on the existing `autonomy.grant` / `grant.list` machinery.
- **Semantic Tool Discovery** (`docs/specs/semantic-tool-discovery.md`): `tool.search` results are already produced from the local index → become automatically permission-filtered. Future embedding backend can apply the same filter before vector search.
- **Autonomy Grants & Court** (`docs/prd/agent-autonomy.md`, governance-court): Permissions compose naturally. Level 2 autonomy can carry broader default tool grants. High-impact permission grants can require Court vote (like autonomy promotion).
- **Channels / Collaboration**: Permission requests, grants, and denials can be posted to a dedicated channel (or #security / #audit) for transparency and Court visibility. `channel.post` itself can be a grantable capability.
- **Network Boundary / LLM calls**: If tool use routes through `llm.*`, the permission check happens before the LLM is asked to emit a tool call, or on the returned action.
- **Web Portal / Dashboard**: Extend existing SecurityPosturePanel, members views, or add a lightweight Permissions tab. Re-use real-time STOMP + contracts already in place.

## Security Requirements (Paranoid First)

- Grants are **never** self-granted by any microVM. Only authorized subjects (user via Portal daemon path, CISO after opt-in, or Court-approved flows) may create them.
- All grant mutations are signed, versioned, and appended to the tamper-evident audit log in Store.
- Revocation is effective within seconds (hot invalidation path via Hub).
- Denied invocations always produce a clear, non-leaking error and a full audit entry; never silent.
- CISO delegation path is **opt-in only** (single user preference flag). While delegated, CISO still cannot grant high-impact capabilities without Court review, and the user retains instant revocation.
- Least-privilege TCB: permission checks should live in the smallest possible components (Hub for routing, local index inside the agent for discovery, Store only for the grant database).
- No ambient authority: even if a tool appears in a prompt or index, the runtime enforcement layer double-checks before execution.
- Threat model coverage: compromised agent cannot escalate its own grants; malicious Hub cannot forge snapshots (signing + verification); user sees exactly what each agent can do.

## Test Requirements

- **Unit**: Permission snapshot filtering logic in `AgentSkillIndex`; `permission.check` / grant CRUD in Store; invalidation + refresh in Hub.
- **Integration**: Start a `project-manager*` with narrow grants (e.g. can `channel.post` and `llm.*` but not `channel.create`); verify `tool.search "create channel"` returns nothing or low-score results, and direct call is rejected with `ERR_PERMISSION_DENIED`. Repeat for a `coder*` instance.
- **E2E / Playwright**: Portal grant/revoke toggle flow; live agent behavior change (tool list in prompt or UI updates); audit log visible; CISO delegation opt-in flow (with mock CISO responses).
- **Security / adversarial**: Attempt by agent to bypass via crafted messages, direct vsock, or index manipulation — all must be rejected and audited.
- **Regression**: Existing ACL flows, autonomy grants, Court reviews, and basic agent tool use continue to work unchanged.

## Traceability

**Driven by:**
- User request for Android-style per-agent/tool permissions with user (or opt-in CISO) control
- Existing ACL deny-by-default + local `AgentSkillIndex` least-privilege foundation
- Autonomy grant + Court graduation model already in Store
- Threat model of over-privileged or compromised agents in a multi-persona collaborative system
- Goal of minimal ongoing user toil while keeping paranoid defaults

**Related Documents:**
- `../prd/permissions-model.md` (high-level goals + UX)
- `aegishub.md`, `store-vm.md`, `agent-runtime.md`, `security-model.md`, `semantic-tool-discovery.md`, `security-boundaries.md` (web-portal)
- `../prd/governance-court.md`, `../prd/agent-autonomy.md`, `../prd/collaboration-model.md`
- Implementation plan collaboration model and existing grant/timer code in Store

## Implementation Notes for `feat/permissions-model` Branch

This spec will be realized in one cohesive set of changes on this branch (docs already committed; code + tests to follow):

1. Extend `internal/agent/skills/index.go` — add permission-filter support to `AgentSkillIndex` and `HandleToolCommand`.
2. Store changes — new grant storage + `permission.*` command handlers (build on autonomy/timer code).
3. AegisHub — snapshot distribution on handshake, invalidation on grant events, extended ACL/permission checks.
4. Minimal Portal UI — grant management surface (can be small extension of existing dashboard/security views).
5. Bootstrap updates in `config/acls.yaml` and initial Store state.
6. Comprehensive tests (unit + integration + e2e with differentiated personas) and audit verification.
7. Update any prompt templates or workspace loader that inject tool lists to respect the filtered index.

Once implementation + testing complete on this branch, a single PR to main.

**Current Status (on this branch):** Specs written. Implementation pending.