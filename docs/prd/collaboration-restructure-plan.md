# AegisClaw Multi-Agent Channels & Collaboration Restructure - Implementation Plan

**Branch**: `feat/multi-agent-channels-collaboration`
**Status**: Draft for review (this PRD-level change)

## Executive Summary

This restructure moves AegisClaw from a per-conversation agent spawning model (with on-demand Court reviews) to a Slack-inspired channels/topics model. 

**Key shifts**:
- Court personas and new SDLC specialists become first-class **Role Agents** that participate in relevant channels.
- A **Project Manager Agent** acts as intelligent orchestrator: plans user requests, decides which agents/roles are needed, delegates work, monitors progress, and triggers Court reviews.
- Agent microVMs (Court Members, SDLC roles, PM, general) are spun **up and down on demand** with a hard target of **<1s startup** to keep local resource usage low while delivering responsive UX and visible governance.

This directly realises needs from the user personas (permanent sub-agent roles, fast visible Court, shared audit history, team-scale collaboration) while solving the "getting quite heavy" problem of spinning full agents per chat.

## Motivation & Alignment with Personas

See `docs/prd/personas.md` and the new `collaboration-model.md` for details. In short:
- **Alex (solo power user)**: Fast Court (<30s visibility), debate/feedback in plain English, low resource footprint on laptop.
- **Sam (team lead)**: Permanent role agents ("MRM Agent" example), All-Hands Court visibility replacing meetings, shared reviewer perspectives.
- **Jordan & Lena**: Exportable tamper-proof logs, scalable from laptop to enterprise, compliance mapping.

Current per-conversation model + independent Court reviews (no debate, 5-vs-7 persona inconsistency) does not fully deliver on these.

## Proposed Model Highlights

1. **Channels as organisational primitive** (per topic/workstream, like Slack channels). Agents join/participate in one or more.
2. **Role-specialised Agents**:
   - Court Agents (7 personas, isolated microVMs, formal review gate).
   - SDLC Agents (Coder, Tester, Requirements, Deployer, etc.).
   - Project Manager Agent (planner, delegator, synthesizer).
   - General on-demand agents.
3. **On-demand microVM lifecycle** managed by Host Daemon + AegisHub. Fast path critical.
4. **PM as the "brain"** for new requests: break down, identify agents/channels, delegate, escalate to Court.
5. **Court integration**: Proposals can originate from channels/PM; reviews visible via summaries/feedback in relevant channels while preserving isolation, unanimous vote rules, and user veto.

## Files to Create / Update / Rename

### New Files (this PR)
- `docs/prd/collaboration-restructure-plan.md` (this file)
- `docs/prd/collaboration-model.md` — The new central PRD/spec for collaboration, channels, roles, PM, and lifecycle. (Supersedes and expands the old conversation-model.md)

### Updated Files (this PR - PRD layer)
- `docs/prd/index.md` — Add new document, update conversation-model reference, fix persona count consistency (7), note migration.
- `docs/prd/runtime-architecture.md` — Add "Dynamic Agent Lifecycle & Resource Management" section; update Sandboxed Components for role-based Agent Runtime VMs and channel concepts.
- `docs/prd/governance-court.md` — Evolve Court personas to Agents in channels; add fast availability, collaborative visibility, fix to 7 personas consistently; integrate with PM.
- `docs/prd/sdlc-governance.md` — Extend SDLC process to include SDLC Agents + PM coordination; reaffirm Court as mandatory gate on all changes.
- `docs/prd/personas.md` — Minor updates to Product Needs and Success Metrics for channel/multi-agent UX and on-demand specialists.
- `docs/prd/glossary.md` — Add new terms (Channel, ProjectManagerAgent, SDLC Agent, Agent Roster, Topic Workspace, etc.).

### Rename / Removal
- `docs/prd/conversation-model.md` — Content migrated to `collaboration-model.md`. Old file deleted in this branch (or noted as superseded).

### Out of Scope for this PR (future work)
- Full updates to `docs/specs/` (agent-runtime.md, host-daemon.md, aegishub.md, chat-ui-data-flow.md, court-scribe.md, store-vm.md, event-system.md, etc.)
- Implementation of fast <1s microVM spin-up, channel state management, PM Agent logic, UI channel support, role prompt templates.
- Migration guide, tests, solo-user defaults.

## General Sections to Include in Updated/New Docs

Across the changed files, incorporate or expand these core topics:

1. **Motivation / Problem Statement** — Per-conversation spawning is resource-heavy and provides poor support for persistent roles, shared history, and visible ongoing Court presence desired by personas.
2. **Proposed Model Overview** — Channels + role Agents + PM orchestrator + on-demand <1s lifecycle.
3. **Agent Types & Roles** — Detailed definitions and responsibilities for Court (7), SDLC, Project Manager, general.
4. **Channel / Collaboration Mechanics** — Definition, membership, messaging (@mentions to roles/agents), threading (proposals/reviews), persistence (Store VM), visibility/ACLs.
5. **Project Manager Agent Role** — Intake, planning, delegation logic, progress monitoring, Court escalation, synthesis.
6. **Court Integration in Channels** — How formal proposals/reviews appear in channel context; visible reasoned feedback while keeping isolation and "any Reject blocks" rule; user veto.
7. **Dynamic Lifecycle & Fast Startup (<1s target)** — Triggers, Host Daemon responsibilities, optimisations (minimal rootfs, parallel launch, warm pools?, snapshot resume), idle spin-down, resource observability/quotas.
8. **Security, Isolation & Audit** — Channel-scoped ACLs via AegisHub, no bypass of Court for changes (even by SDLC agents), auditable delegation, threat model updates.
9. **UX & UI Implications** — Channel roster, agent presence, composer @mentions, inline Court summaries, plan visibility.
10. **Example Journeys & Workflows** — Concrete end-to-end for simple request and complex feature (PM delegation + Court review in channels).
11. **Migration & Defaults** — How old conversations map; sensible solo defaults (e.g. single "main" channel with core Court + PM).
12. **Open Questions & Risks** — Debate vs independent review; Court state persistence; complexity for solo users; warm-pool vs pure cold-start tradeoff.

## Phased Implementation Recommendation

**Phase 1 (this PR)**: Requirements & model clarity (PRD updates).
**Phase 2**: Specs layer (detailed technical design for runtime, hub, UI, agents).
**Phase 3**: Core runtime changes (Host Daemon fast lifecycle mgmt, AegisHub channel routing & ACLs, Store for channel state).
**Phase 4**: Agent specialisation (role templates/prompts/souls for Court & SDLC, PM Agent ReAct/planning loop).
**Phase 5**: Portal/UI (channel sidebar, roster, @mentions, Court cards, delegation views).
**Phase 6**: Polish, performance validation (<1s measured), E2E tests (Playwright), solo vs team defaults, migration tooling.

## Open Questions (to resolve in Specs/Impl PRs)
- Inter-agent comms in channels: fully mediated via Hub/events only, or limited direct? Formal proposals vs richer "debate" chat for Court personas?
- Court UX: inline visible feedback/threads in channel, or background process with summaries posted?
- Solo user experience: auto-provision "Personal" or "Main" channel with core agents always fast-available?
- Court Member VM identity: one long-lived per persona type, or fresh per review for maximum isolation?
- Warm standby pools for Court/PM vs pure on-demand (complexity vs cold-start latency)?
- How "permanent roles" are represented (dedicated channel membership vs dynamic invitation by PM)?

## Success Criteria for this Change
- PRD docs clearly specify the new model and align with all four personas' stated needs.
- <1s startup target is explicit and measurable in runtime docs.
- Governance guarantees (Court gate on every change, isolation, audit) are preserved or strengthened.
- Clear migration path and sensible defaults for existing users (esp. solo).
- Foundation ready for efficient, visible, role-specialised multi-agent collaboration.

---

*This plan was generated as part of the initial restructure exploration. Feedback welcome on branch or in PR.*