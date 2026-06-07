# Collaboration Model

**How users, the Project Manager, Court personas, and SDLC specialists work together in channels with on-demand agents.**

## 1. Motivation & Problem with Current Model

The existing approach of spinning a new general agent for each conversation (with Court members invoked on-demand for proposals) has become resource-heavy on local hardware. It provides limited support for:

- Persistent specialised roles (explicitly requested by Sam Chen persona: "sub-agents that can be assigned permanent roles").
- Visible, ongoing Court presence and shared reviewer perspectives across a team or long-running work.
- Natural collaboration, history, and threading around topics.
- Efficient delegation and planning for complex requests.

Personas (see `personas.md`) want fast Court visibility (<30s), plain-English debate/feedback, permanent role agents, exportable audit trails, and a system that scales from solo laptop to team/enterprise without extra tooling. The current model does not fully deliver this.

## 2. Proposed Model Overview

AegisClaw adopts a **Slack-inspired channels + role-based multi-agent** model:

- **Channels** (or "topics"/workspaces) are the primary organisational unit for work, conversations, and collaboration.
- **Specialised Agents** participate in one or more channels as needed:
  - **Court Agents**: The 7 Governance Court personas run as first-class isolated Agent instances.
  - **SDLC Agents**: Role-specific agents for requirements, coding, testing, deployment, etc.
  - **Project Manager Agent**: The central orchestrator and planner.
  - General on-demand agents for ad-hoc tasks.
- **On-demand microVM lifecycle**: Agents (and their Court/SDLC/PM instances) are spun up when needed and spun down when idle, targeting **<1s startup** for responsive UX and low resource use on local machines (critical for Alex persona's laptop).
- **Project Manager** receives user requests, creates plans, determines required agents/roles/channels, delegates work, monitors, synthesises results, and escalates to Court via formal proposals when changes are involved.

All execution remains inside isolated Firecracker microVMs (or Docker sandboxes). AegisHub continues as the strict ACL router. Court remains the mandatory, non-bypassable gate on *every* code change, skill, prompt, or autonomy modification.

## 3. Channels & Collaboration Mechanics

**Channel** = a named, persistent conversation space focused on a topic, project, or workstream (e.g. "governance", "feature-auth", "personal-automation").

- Users and agents can be members of multiple channels.
- Channel history, proposals, and key artifacts are persisted (Store VM).
- Messaging supports human text + agent posts, **@mentions** to specific agents or roles (e.g. `@ProjectManager`, `@CISO`, `@Coder`).
- Threading recommended for focused work (especially proposals and Court reviews).
- Visibility and participation can be scoped; AegisHub enforces channel-aware ACLs.
- Court reviews and votes are recorded in the tamper-evident audit log; summaries or reasoned feedback can be posted back into relevant channel(s) for visibility (while formal voting stays isolated).

Channels provide the "shared governance history" and "All-Hands Court" visibility that Sam and other personas want, without requiring everyone to be in one giant workspace.

## 4. Agent Roles

### Court Agents (7 Personas)
Each runs in its own isolated microVM (as today):

1. **CISO** — Business risk, compliance, security posture.
2. **Security Architect** — Deep technical security (attack surface, escapes, escalation).
3. **Architect** — System design, modularity, scalability, maintainability.
4. **Senior Coder** — Code quality, readability, standards, correctness.
5. **Tester** — Test coverage, edge cases, validation, reliability.
6. **Efficiency** — Performance, resource usage, latency, cost.
7. **User Advocate** — Real user value, UX impact.

They participate in a dedicated "governance" channel (or relevant topic channels) for visibility. Reviews are triggered by formal proposals (from PM, users, or SDLC agents). They post reasoned votes/feedback (`Approve` / `Reject` / `Abstain` / `Needs Work`). Unanimous non-abstain Approve (or any Reject) rules remain. User has final veto.

### SDLC Agents (examples)
Specialised operational agents that can be invited into feature or dev channels by the PM:
- Requirements / Planner
- Coder / Implementer
- Tester / QA
- Reviewer / Static analysis
- Deployer / Release

All changes they produce still require formal Court proposal + review + final sign-off. No bypass.

### Project Manager Agent (new central role)
Responsibilities:
- Ingest user natural-language requests or high-level goals.
- Break down into actionable plan (tasks, required roles, suggested channels).
- Decide which agents to spin up / invite to which channels.
- Delegate work (post tasks or @mention in channel).
- Monitor progress, unblock, re-plan as needed.
- Synthesise results for user.
- Identify when formal proposals / Court review is required and initiate them.
- Maintain visibility of overall plan and status (perhaps as artifacts in channel or dedicated view).

The PM is the "intelligent glue" that makes the multi-agent system usable for complex work without the user having to manually orchestrate everything.

### General Agents
On-demand instances for specific, short-lived tasks. Spun up by PM or user as needed.

## 5. Dynamic Agent Lifecycle & Resource Management

**Core requirement**: Agent microVMs (including Court and PM) must support **fast startup (<1s target)** and clean spin-down when idle.

Host Daemon responsibilities expanded:
- Fast launch path for Agent Runtime VMs, Court Member VMs, and specialised role instances.
- Idle detection and graceful spin-down (timeout-based or explicit).
- Resource accounting and limits (per channel, per user, or global).
- Observability into which agents are active and why.
- Possible warm pools or snapshot resume for the most common agents (Court personas, PM) to hit <1s reliably on laptop-class hardware.

This is essential for Alex's success metric (Court in <30s) and for keeping the system lightweight when not in active use.

Triggers for spin-up:
- PM decides an agent/role is needed for current plan.
- User explicitly requests or @mentions a role.
- Channel activity that requires a specialist.
- Court review requested.

## 6. Project Manager Orchestration Flow (Example)

1. User: "Implement OAuth2 login with Court review and tests."
2. PM creates plan: tasks, required channels (e.g. "auth-feature"), roles (Requirements, Coder, Tester, Court for proposal).
3. PM spins/invites appropriate agents to the channel.
4. Work happens in channel (posts, code snippets, test results, discussions).
5. When ready for change: PM (or Coder) creates formal Change Proposal.
6. Proposal posted to governance channel or directly to Court Agents.
7. Court Agents review (visible summaries/feedback appear in feature channel).
8. If approved + user veto: SDLC agents implement + final Court sign-off.
9. PM synthesises status and notifies user.

All steps auditable. Court gate never bypassed.

## 7. Security, Isolation & Governance Guarantees

- Every component that touches untrusted data or generates code still runs in its own isolated microVM.
- Court Members **cannot** see each other's memory or state (isolation preserved).
- AegisHub enforces strict, now channel-aware ACLs.
- **No change** (code, skill, prompt, autonomy, Court config) is deployed without formal proposal + Court review + user veto.
- Delegation by PM is logged and auditable.
- Channel participation does not grant extra privileges; agents only act within their role and channel scope.

This maintains (and arguably strengthens) the paranoid security model while adding collaborative visibility.

## 8. User Experience & UI Considerations

- Channel list / sidebar (like Slack or Discord).
- Per-channel **agent roster** showing active/idle/present roles and specific agents.
- Composer with @role or @agent autocomplete and mention support.
- Inline cards or threads for Court review status, votes, and summaries.
- Plan / delegation view (perhaps as pinned artifact or dedicated panel).
- Notifications for Court decisions, task updates, or @mentions.
- Fast perceived availability: even if a specialist spins up, <1s + good loading states make it feel instant.

Solo users get sensible defaults (e.g. a single "main" or "personal" channel pre-provisioned with core Court + PM always fast-available).

## 9. Migration from Previous Model

- Existing conversations can be mapped to a default "general" or "inbox" channel.
- Old single-agent sessions become lightweight entries in the new model.
- Court review history and audit logs are preserved.
- Users can continue using simple direct chat (PM acts as default entrypoint and hides complexity).

A future migration tool or one-time import can be considered in implementation phase.

## 10. Open Questions & Future Work

- How much "free chat" vs strict proposal-based interaction between Court agents in channels? (Rich feedback desired by personas vs strict isolation.)
- Court state: long-lived persona instances vs fresh per review?
- Warm pools vs pure on-demand for <1s target (trade-off complexity vs cold-start perf).
- Exact representation of "permanent roles" (channel membership vs dynamic PM invitation).
- Solo-user simplification vs full multi-channel power.

These will be resolved in the Specs and Implementation phases.

## 11. Related Documents

- `personas.md` / `user-personas.md` — User needs this model serves.
- `governance-court.md` — Detailed Court process.
- `sdlc-governance.md` — How SDLC changes are still gated.
- `runtime-architecture.md` — Dynamic lifecycle details.
- `glossary.md` — New terms added.
- `../specs/` — Future detailed technical specs (agent-runtime, host-daemon, aegishub, chat-ui, etc.).

---

*This model better turns AegisClaw's strong isolation and Court primitives into a usable, visible, role-specialised team while keeping the paranoid guarantees intact.*