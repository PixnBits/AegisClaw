# Permissions & Capability Grants Model

**Status:** Implemented on `feat/permissions-model` branch (CISO delegation opt-in slice added)

AegisClaw agents and specialized microVMs operate under **least-privilege by default**. Each component (Project Manager, Coder, CISO persona, Court members, generic agents, etc.) is granted only the exact capabilities (tools/commands) it needs. Grants are decided explicitly by the user — or later delegated to the CISO persona after explicit opt-in.

This model is directly inspired by Android's runtime permissions for apps: declaration + user grant/deny decision + runtime enforcement. Applied here to isolated AI agents and microVMs for paranoid, auditable security.

## Goals

- No agent or microVM can discover or invoke a capability it has not been explicitly authorized for (e.g. a Coder agent must not be able to call `channel.create` or `proposal.submit` unless granted).
- Agents can **discover** what tools exist in the system in a controlled, non-leaking way so they can intelligently request the permissions they need.
- An attempt by an agent to use a tool it is not granted triggers a clear **permission request** that the user can review and act on in the Web Portal (Agents page → specific agent).
- Grants (and visibility) are visible, revocable, and fully audited in the tamper-evident Store log.
- User toil is minimized over time via an **opt-in-only** delegation path to the CISO persona (which itself operates under Court oversight).
- The system integrates cleanly with existing foundations: ACL routing in AegisHub, local `AgentSkillIndex` in every agent, Store as source of truth, signed vsock messaging, and autonomy grants.

## Core Concepts

- **Capability**: Any `noun.verb` command or skill tool (e.g. `channel.create`, `store.create_channel`, `proposal.submit`, `discord_monitor.send_message`, scoped `llm.call`).
- **Subject**: A specific microVM ID (e.g. `project-manager-abc123`) **or** a persona-type wildcard (`project-manager*`, `coder*`).
- **Grant**: An explicit allow record stored durably in Store. Default is deny-everything.
- **Visibility Policy**: Separate from grants. Controls whether a tool even *appears* in discovery results for a given subject (even if the subject does not yet have a grant). User can hide sensitive tools from specific agents/personas to prevent fingerprinting.
- **Enforcement points**: AegisHub (cross-VM), Agent Runtime local index + invocation checks (in-process), and Store (for grant operations themselves).

## Safe Discovery (Solving "Agent Doesn't Know What to Ask For")

A core tension exists: an agent cannot request permission for a tool whose existence it does not know.

**Solution**:

- `tool.list` and `tool.search` (the commands every agent already uses inside its 6-step loop) **always** return only the tools the agent is **currently granted**, plus any tools the user has explicitly marked as "publicly discoverable" for that persona type.
- There is a separate, **grantable** capability called `tool.registry.discover` (or `capability.discover`). When an agent holds this grant, it can perform a broader discovery query. Results are still filtered by the user's **Visibility Policy** for that subject.
- This gives agents enough information to be useful without exposing the full global tool surface to every agent (mitigating fingerprinting and information leakage).

## Permission Request Flow on Tool Use Attempt

When an agent (during reasoning, direct command, or LLM-generated tool call) attempts to use a capability it does **not** currently hold a grant for:

1. The runtime / Hub / Store enforcement layer rejects the call with a clear `ERR_PERMISSION_DENIED` (the agent sees this in its context).
2. A structured `permission.request` event is emitted containing: requesting subject, desired capability, context/reason the agent provided (if any), and timestamp.
3. This request appears in the **Web Portal under Agents → [specific agent]** in a dedicated "Permission Requests & Denied Attempts" section.
4. The user can:
   - Grant the capability immediately
   - Deny (optionally with a note)
   - Hide the tool from this agent (updates Visibility Policy)
   - Delegate the decision to the CISO persona (if delegation is opted-in)

This flow makes the system responsive: agents can "ask" for what they need through action, and the user stays in control via a clear, per-agent review surface.

## User Experience

- **Defaults**: Extremely tight. Only minimal bootstrap grants exist in `config/acls.yaml` and initial Store state. Most tools start hidden or request-only.
- **Portal / CLI**: 
  - Clear view of active agents/personas.
  - Per-agent page shows: currently granted tools, pending permission requests, recent denied attempts (with context), and visibility/hide controls.
  - Simple toggles to grant/revoke/hide.
  - "Why this capability?" descriptions come from tool metadata.
- **Immediate effect**: Grant or visibility changes invalidate the affected agent's local tool index within seconds.
- **Audit**: Every grant, revoke, hide change, and denied invocation appears in the Store audit log (visible in Portal Audit view).
- **CISO Delegation (opt-in only)**: In Settings the user can flip a single switch to allow the CISO persona to receive and propose routine permission grants (and visibility decisions). High-impact items still route through Court review. The user can revoke delegation instantly. Default = off. (Implemented as persisted flag + gated grant allowance for ciso* sources.)

## Visibility & Anti-Fingerprinting Controls

Allowing broad tool discovery creates a competing concern: a compromised or curious agent could fingerprint the environment by learning every available skill and tool, even ones it will never be granted.

**Mitigation**:

- Visibility Policy is a first-class, user-controllable layer **separate from grants**.
- For any tool (or entire skill), the user (or opted-in CISO) can set visibility rules such as:
  - Hidden from everyone (never appears in any discovery)
  - Visible only to subjects that already hold a grant
  - Visible to specific personas / subjects (so they can discover and request access)
  - "Requestable" flag (appears with a clear "request access" indicator)
- These rules are stored alongside grants in Store and applied at every discovery point (`tool.list`, `tool.search`, `tool.registry.discover`).
- High-sensitivity tools (e.g. court administration, certain store admin commands, secret management) can be hidden by default from all non-Court personas.

This gives the user fine-grained control: a Coder agent can be prevented from ever learning about the existence of high-privilege tools, while still being able to discover and request the tools it legitimately needs.

## Philosophy

Trust is earned, narrowly scoped, and never assumed. An agent should behave like a trusted specialist colleague with a defined remit — not like a general-purpose administrator. The architecture makes overreach structurally impossible rather than relying on prompt discipline or runtime monitoring alone.

Discovery is itself a controlled, grantable, and visibility-filtered capability. Attempted overreach surfaces as actionable, reviewable requests rather than silent failures or hidden knowledge.

This directly extends the existing autonomy graduation model (Level 0–2) and the Governance Court process.

## Related Documents

- [specs/permissions-model.md](../specs/permissions-model.md) — Detailed technical specification (commands, enforcement flows, integration points, visibility policy)
- [specs/security-model.md](../specs/security-model.md), [specs/aegishub.md](../specs/aegishub.md), [specs/store-vm.md](../specs/store-vm.md), [specs/agent-runtime.md](../specs/agent-runtime.md), [specs/semantic-tool-discovery.md](../specs/semantic-tool-discovery.md)
- [prd/security-model.md](./security-model.md), [prd/governance-court.md](./governance-court.md), [prd/collaboration-model.md](./collaboration-model.md), [prd/agent-autonomy.md](./agent-autonomy.md)
- Existing autonomy grants, timers, and Court Scribe machinery in Store
- User Journey #7 (granting/adjusting autonomy)

## Implementation Approach on This Branch

Docs first (this PRD + detailed spec). Then a single cohesive implementation on the same `feat/permissions-model` branch covering:

- Store persistence for grants + visibility policies + new `permission.*` and `tool.registry.*` commands
- Hub handshake + snapshot distribution + invalidation
- Extension of `AgentSkillIndex` to respect both permission grants **and** visibility filters
- Permission request emission on denied tool attempts + Portal UI for review (Agents → specific agent)
- Visibility/hide controls in Portal
- Minimal updates to `config/acls.yaml` bootstrap rules
- E2E tests with differentiated PM vs Coder personas, including fingerprinting-mitigation scenarios
- Audit log integration

No backward-compatibility concerns (pre-alpha). One branch, one PR to main after testing.