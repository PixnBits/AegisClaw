# Permissions & Capability Grants Model

**Status:** Draft — to be implemented on `feat/permissions-model` branch

AegisClaw agents and specialized microVMs operate under **least-privilege by default**. Each component (Project Manager, Coder, CISO persona, Court members, generic agents, etc.) is granted only the exact capabilities (tools/commands) it needs. Grants are decided explicitly by the user — or later delegated to the CISO persona after explicit opt-in.

This model is directly inspired by Android's runtime permissions for apps: declaration + user grant/deny decision + runtime enforcement. Applied here to isolated AI agents and microVMs for paranoid, auditable security.

## Goals

- No agent or microVM can discover or invoke a capability it has not been explicitly authorized for (e.g. a Coder agent must not be able to call `channel.create` or `proposal.submit` unless granted).
- Grants are visible, revocable, and fully audited in the tamper-evident Store log.
- User toil is minimized over time via an **opt-in-only** delegation path to the CISO persona (which itself operates under Court oversight).
- The system integrates cleanly with existing foundations: ACL routing in AegisHub, local `AgentSkillIndex` in every agent, Store as source of truth, signed vsock messaging, and autonomy grants.

## Core Concepts

- **Capability**: Any `noun.verb` command or skill tool (e.g. `channel.create`, `store.create_channel`, `proposal.submit`, `discord_monitor.send_message`, scoped `llm.call`).
- **Subject**: A specific microVM ID (e.g. `project-manager-abc123`) **or** a persona-type wildcard (`project-manager*`, `coder*`).
- **Grant**: An explicit allow record stored durably in Store. Default is deny-everything.
- **Enforcement points**: AegisHub (cross-VM), Agent Runtime local index + invocation checks (in-process), and Store (for grant operations themselves).

## User Experience

- **Defaults**: Extremely tight. Only minimal bootstrap grants exist in `config/acls.yaml` and initial Store state.
- **Portal / CLI**: Clear view of active agents/personas in a session or team. Shows tools they currently have, tools they have requested (or that exist in the registry), and simple toggles to grant/revoke. "Why this capability?" descriptions come from the tool metadata.
- **Immediate effect**: Grant changes invalidate the affected agent's local tool index within seconds; the agent sees only permitted tools in reasoning and `tool.search`.
- **Audit**: Every grant, revoke, and denied invocation appears in the Store audit log (visible in Portal Audit view).
- **CISO Delegation (opt-in only)**: In Settings the user can flip a single switch to allow the CISO persona to receive and propose routine permission grants. High-impact grants still route through Court review. The user can revoke delegation instantly. Default = off.

## Philosophy

Trust is earned, narrowly scoped, and never assumed. An agent should behave like a trusted specialist colleague with a defined remit — not like a general-purpose administrator. The architecture makes overreach structurally impossible rather than relying on prompt discipline or runtime monitoring alone.

This directly extends the existing autonomy graduation model (Level 0–2) and the Governance Court process.

## Related Documents

- [specs/permissions-model.md](../specs/permissions-model.md) — Detailed technical specification (commands, enforcement flows, integration points)
- [specs/security-model.md](../specs/security-model.md), [specs/aegishub.md](../specs/aegishub.md), [specs/store-vm.md](../specs/store-vm.md), [specs/agent-runtime.md](../specs/agent-runtime.md), [specs/semantic-tool-discovery.md](../specs/semantic-tool-discovery.md)
- [prd/security-model.md](./security-model.md), [prd/governance-court.md](./governance-court.md), [prd/collaboration-model.md](./collaboration-model.md), [prd/agent-autonomy.md](./agent-autonomy.md)
- Existing autonomy grants, timers, and Court Scribe machinery in Store
- User Journey #7 (granting/adjusting autonomy)

## Implementation Approach on This Branch

Docs first (this PRD + detailed spec). Then a single cohesive implementation on the same `feat/permissions-model` branch covering:

- Store persistence and new `permission.*` commands
- Hub handshake + snapshot distribution + invalidation
- Extension of `AgentSkillIndex` to respect permission snapshots
- Minimal Web Portal UI (or extension of existing SecurityPosture / members views)
- Updates to `config/acls.yaml` bootstrap rules
- E2E tests with differentiated PM vs Coder personas
- Audit log integration

No backward-compatibility concerns (pre-alpha). One branch, one PR to main after testing.