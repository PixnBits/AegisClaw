# Permissions Model Specification

**Status:** Implemented on `feat/permissions-model` branch (CISO delegation opt-in slice included)
**Last Updated:** 2026-06-24

## Purpose

Define a fine-grained, user-controlled, auditable capability-grant system so every agent and microVM can only discover and invoke the exact tools/commands it has been authorized for. This is the technical realization of the Android-style permissions model described in the companion PRD, including safe discovery, permission requests on use attempts, and visibility controls to mitigate fingerprinting.

## Responsibility Boundaries

**Store VM owns (source of truth):**
- Durable, signed storage of grants **and visibility policies** (alongside autonomy grants and timers; tamper-evident Merkle audit log)
- New/extended commands: `permission.grant`, `permission.revoke`, `permission.list`, `permission.check`, `permission.snapshot`, `permission.request` (event), plus visibility policy commands (`visibility.set`, `visibility.get`, `visibility.list`)
- `tool.registry.discover` handling (when the calling subject holds the grant)
- Grant/revoke/visibility authorization checks (user via Portal, CISO after opt-in, or Court for high-impact)
- Publishing of `permission.granted` / `permission.revoked` / `permission.denied` / `permission.request` events via Hub
- Reconciliation and audit of denied attempts and visibility changes

**AegisHub owns:**
- Extension of ACL enforcement to include fine-grained permission checks
- Distribution of signed permission + visibility snapshots to microVMs during/after registration handshake
- Hot-reload / EventBus-driven invalidation when grants or visibility policies change
- Rejection with clear `ERR_PERMISSION_DENIED` for unauthorized attempts; emission of `permission.request` events on denied tool use

**Agent Runtime (and persona microVMs) owns:**
- Local filtering of `AgentSkillIndex` using **both** the permission grant snapshot **and** the visibility policy for that subject
- Runtime enforcement before any tool/skill invocation
- Emitting `permission.request` events (with context) when a tool use is denied due to missing grant
- Clear error surfaces in the 6-step loop

**Web Portal / CLI owns:**
- Primary grant/revoke/hide UI, with a dedicated per-agent view showing current grants, pending requests, recent denied attempts (with agent-provided context), and visibility controls
- Explicit opt-in toggle for CISO delegation (stored in user preferences / Store)

**CISO Persona (post explicit user opt-in only):**
- Receives `permission.request` events
- Can propose grants **and** visibility changes via the appropriate Store commands
- Still subject to Court review for high-impact items; user retains instant revocation of delegation

## Core Model

- **Capability** = any `noun.verb` command or registered skill tool.
- **Subject** = microVM instance ID or persona-type pattern.
- **Grant** = allow record: `{subject, capability, granted_by, granted_at, reason?, conditions?}`.
- **Visibility Policy** = first-class rules controlling whether a tool appears in discovery results for a subject (independent of whether the subject holds a grant). Examples: hidden from everyone, visible only to granted subjects, visible to specific personas for request purposes, "requestable" flag.
- **Default policy**: Deny everything + hide by default except explicit bootstrap grants and minimal public visibility in `config/acls.yaml` + initial Store state.
- Grants and visibility policies are **additive** to the existing ACL layer.

## Discovery Commands & Safe Exposure

- `tool.list` / `tool.search` (existing, used inside every agent's reasoning loop) → return only tools the subject is **currently granted** + any tools explicitly marked as publicly discoverable for that persona type. These are **always safe** to expose.
- `tool.registry.discover` (new, itself a grantable capability) → when the subject holds this grant, allows broader queries against the global registry. Results are **still filtered** by the subject's current Visibility Policy. Sensitive implementation details can be redacted in responses.
- This design solves the "agent cannot request what it does not know exists" problem while directly mitigating fingerprinting: a Coder agent can be configured (via visibility rules) never to learn about the existence of Court or high-privilege admin tools.

## New / Extended Commands

All routed through AegisHub with signature + ACL validation, then handled primarily by Store.

**Permission commands:**
- `permission.grant`, `permission.revoke`, `permission.list`, `permission.check`, `permission.snapshot` (subject → current allow-set + visibility summary)
- `permission.request` event (emitted on denied tool attempt; contains subject, capability, context)

**Visibility commands:**
- `visibility.set` (subject or pattern, capability or skill, visibility_level or hide flag, [reason])
- `visibility.get` / `visibility.list`

High-impact grants or visibility changes (anything affecting Court, secrets, or cross-agent state) can require Court review before Store accepts them.

## Enforcement & Request Flow (End-to-End)

1. MicroVM starts/reconnects → Hub handshake → Hub fetches permission snapshot + visibility policy from Store → pushes signed snapshot to the microVM.
2. Agent Runtime filters its local `AgentSkillIndex` (and any `tool.search` / `FormatAvailableTools` paths) using **both** grants and visibility rules. Only permitted + visible tools appear in reasoning or `tool.list`/`tool.search`.
3. When the agent attempts a tool call (via act/execute layer or LLM tool emission) for a capability it does **not** hold a grant for:
   - Call is rejected with `ERR_PERMISSION_DENIED`.
   - Structured `permission.request` event is emitted (subject, capability, agent-provided context/reason, timestamp).
   - Event is logged to Store audit and made available to the Portal (per-agent view) and (if opted-in) to the CISO persona.
4. Grant or visibility change → Store records it → publishes events → Hub pushes invalidation/update → affected agents refresh their local indexes immediately.
5. All denied attempts, grants, revokes, and visibility changes are appended to the tamper-evident audit log.

## Integration with Existing Components

- **AgentSkillIndex** (`internal/agent/skills/index.go`): Now applies **two filters** on every `SearchTools`, `ListSkills`, `HandleToolCommand`, and `FormatAvailableTools` call: (1) permission grants, (2) visibility policy for the subject. The existing Jaccard/substring/Levenshtein scoring works on the filtered set. This is the primary local enforcement + safe-discovery surface.
- **AegisHub + ACLs**: ACLs remain the coarse routing gate. Permissions + visibility add the fine-grained layer. New ACL entries needed for `permission.*`, `visibility.*`, and `tool.registry.discover`.
- **Store VM**: Add storage for visibility policies (alongside grants). Implement new commands and event emission. Build directly on existing autonomy/grant machinery.
- **Semantic Tool Discovery**: `tool.search` results (and future embedding search) are produced from the already-filtered local index → automatically respect both grants and visibility.
- **Autonomy Grants & Court**: Visibility policies and permission requests can be reviewed/promoted through the same Court process as autonomy levels.
- **Channels / Collaboration**: `permission.request` events and visibility changes can be posted to dedicated channels (#permissions, #security, #audit) for transparency.
- **Web Portal / Dashboard**: Extend SecurityPosture or add per-agent detail view showing grants, pending `permission.request` items (with context), denied attempts, and visibility/hide toggles. Re-use existing real-time contracts and STOMP.
- **Network Boundary / LLM**: Permission check happens before LLM is asked to emit tool calls, or on the returned action. Denied LLM tool calls can still emit `permission.request`.

## Security Requirements (Paranoid First)

- Grants and visibility policies are **never** self-set by any microVM.
- All mutations are signed, versioned, and audit-logged in Store.
- Revocation and visibility hide changes are effective within seconds via hot invalidation.
- Denied tool attempts always produce `ERR_PERMISSION_DENIED` + full `permission.request` event; never silent.
- **Fingerprinting mitigation is explicit**: Visibility Policy + the fact that broad `tool.registry.discover` is itself a grantable + visibility-filtered capability directly limits what any agent can learn about the global tool surface.
- CISO delegation is **opt-in only** (single preference flag). Even when delegated, high-impact decisions require Court review; user can revoke instantly.
- Least-privilege TCB: checks live in Hub (routing), local `AgentSkillIndex` (discovery), and Store (policy database). Double-check at invocation time even if something appears in the filtered index.
- Threat coverage: compromised agent cannot learn about or request hidden tools; cannot forge snapshots; user (and Court) see exactly what each agent knows and has tried to do.

## Test Requirements

- **Unit**: Dual filtering (grants + visibility) in `AgentSkillIndex`; visibility policy CRUD and application; `permission.request` emission on denied calls.
- **Integration**: `project-manager*` with narrow grants + restrictive visibility sees only its allowed tools and cannot discover hidden high-privilege tools. `coder*` with `tool.registry.discover` grant still has its view filtered by visibility rules. Attempt to use un-granted tool emits `permission.request` and is reviewable in simulated Portal view.
- **E2E / Playwright**: Per-agent page in Portal shows pending requests + denied attempts + hide controls; toggling grant/visibility immediately affects agent's `tool.search` results and prompt content; CISO delegation opt-in flow.
- **Security / adversarial**: Attempts to bypass via index manipulation, direct messaging, or broad discovery queries are rejected/audited. Fingerprinting attempts on hidden tools are blocked by visibility policy.
- **Regression**: Existing ACLs, autonomy, Court, and basic agent tool use remain unaffected.

## Traceability

**Driven by:**
- User request for Android-style per-agent/tool permissions with user (or opt-in CISO) control
- Need for safe discovery so agents can request what they need without prior knowledge
- Permission request flow on actual tool-use attempts (reviewable per-agent in Portal)
- Fingerprinting / information-leakage concern when agents can list tools; solved via explicit Visibility Policy + gated broad discovery
- Existing ACL deny-by-default + local `AgentSkillIndex` foundation
- Autonomy grant + Court model in Store
- Goal of minimal user toil while preserving paranoid defaults

**Related Documents:**
- `../prd/permissions-model.md` (high-level goals + UX + discovery + request flow + visibility)
- `aegishub.md`, `store-vm.md`, `agent-runtime.md`, `security-model.md`, `semantic-tool-discovery.md`
- `../prd/governance-court.md`, `../prd/agent-autonomy.md`, `../prd/collaboration-model.md`

## Implementation Notes for `feat/permissions-model` Branch

Docs updated with discovery, request-on-attempt, and visibility controls. Implementation on this branch includes:

1. `internal/agent/skills/index.go` — dual filter (grants + visibility policy) in `SearchTools`, `ListSkills`, `HandleToolCommand`, and prompt helpers.
2. Store — storage for visibility policies + new `permission.*` / `visibility.*` / `tool.registry.discover` handlers (reuse autonomy/grant patterns) + CISO delegation flag.
3. AegisHub — snapshot distribution (grants + visibility), invalidation on changes, `permission.request` emission on denied tool calls.
4. Web Portal — per-agent detail view for requests, denied attempts (with context), grant/revoke/hide controls + Settings toggle for CISO delegation.
5. Bootstrap rules in `config/acls.yaml` and initial Store state (minimal public visibility + requestable tools).
6. Comprehensive tests (unit + integration + e2e) covering safe discovery, request flow, anti-fingerprinting, delegation, and UI actions with differentiated personas.
7. Updates to any prompt/workspace injection paths to respect the dual-filtered index.

CISO delegation (opt-in) implemented as a persisted flag; court-persona-ciso sources may propose when enabled (high-impact Court routing deferred per non-goals).

Once implementation + testing complete on this branch, a single PR to main.

**Current Status (on this branch):** Full implementation complete (grants + visibility + delegation opt-in + Portal review + tests + docs). Ready for PR review.