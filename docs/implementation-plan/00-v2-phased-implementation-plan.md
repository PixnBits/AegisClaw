# AegisClaw v2: Complete Phased Implementation Plan

**Branch:** `docs/lessons-learned`  
**Date:** 2026 (post-lessons-learned reference architecture)  
**Status:** Authoritative plan to reach spec-compliant, working system  
**Primary References:** Current `docs/specs/`, `docs/prd/`, `docs/architecture.md`, `docs/testing-standards.md`, `AGENTS.md` (local), and the component-split code in this branch as the *best available reference* (not to be preserved verbatim).

**Detailed Execution Plan:** See the companion `full-implementation-roadmap.md` (created after deep PRD/Spec exploration + Phase 3 progress). It provides granular tasks, traceability to every user journey's Success Criteria, and refines Phases 4–7 for execution.

## 1. Executive Summary & Context

This branch contains the **architectural reference implementation** created after lessons from an earlier monolithic design. The specifications in `docs/specs/` (especially `host-daemon.md`, `web-portal-*.md`, component specs, and the 9 user journeys) and `docs/prd/` have been refined based on those lessons.

**Core Directive:** Implement the system **according to the current specifications**, treating the code here as:
- A proven **component split architecture** (separate `cmd/*` binaries for strong isolation boundaries).
- Partial implementations and skeletons that can be **reused or lightly adapted**.
- **Not** sacred — significant rewrites are expected where drift has occurred (notably the web portal and parts of the Host Daemon).

**Non-negotiables (from testing-standards.md and specs):**
- ≥80% unit test coverage for new code.
- **Every user journey (1-9) must have automated integration/E2E tests before the feature/journey is considered "done".**
- Host Daemon TCB is deliberately minimal and is the **only** privileged component.
- Follow `AGENTS.md` exactly for daemon controls: `sudo ./bin/aegis start ...` (or `make start`), `./bin/aegis stop` (no sudo), and the build-microvms script.

**Prioritization:** Host Daemon / TCB work **first** — it is the foundation of the entire security model.

## 2. Current State Assessment (Deep Exploration Summary)

**Project Root (toplevel):** `/home/pixnbits/projects/AegisClaw/docs/lessons-learned` (self-contained on this branch).

**Key Directory Highlights:**
- `cmd/`: Strong component split (excellent reference):
  - `aegis/` — Host Daemon (cobra CLI, orchestrator, socket server ~500 LOC main + helpers).
  - `aegishub/`, `agent/`, `builder/`, `court-persona/`, `court-scribe/`, `memory/`, `network-boundary/`, `secrets/`, `store/`, `web-portal/` — one binary per sandboxed role.
- `internal/`:
  - `sandbox/` — Factory + Linux (Firecracker) + darwin/windows (Docker) backends. Vsock/port config partial. Reusable foundation.
  - `runtime/orchestrator.go` — VM lifecycle skeleton (Start/Stop/List/Shutdown via backend).
  - `security/manager.go` — Ed25519 keypair for daemon (extend for per-VM distribution).
  - `config/config.go` — Platform/sandbox detection, paths (some `/tmp` vs `~/.aegis` drift).
- `docs/specs/` — Rich, authoritative:
  - `host-daemon.md` (65 LOC, crystal clear: 5 responsibilities + explicit non-responsibilities + test reqs).
  - `web-portal-screens.md` + `web-portal-vm.md` (detailed wireframes, thin-presentation-only rules).
  - 9 user-journey specs (each with Overview, User Story, Step-by-Step, Success Criteria/Testable).
  - Component specs: `aegishub.md`, `agent-runtime.md` (6-step loop), `builder-security-gates.md` (5 mandatory gates), `store-vm.md`, `memory-vm.md`, etc.
  - `cli.md`, `threat-model.md`, `security-model.md`.
- `docs/prd/` — Focused, lesson-refined requirements.
- `docs/implementation-plan/` — Old 25-phase plan (monolithic era, useful for ordering lessons but superseded).
- `e2e/` — Playwright (chat.spec.js, journeys.spec.js covering 4 journeys superficially with fixtures).
- `scripts/build-microvms-docker.sh`, `Makefile` (builds all 11 binaries + microvms; `make start/stop` align well with AGENTS except naming/priv details).
- ~10 `_test.go` files (mostly per-cmd unit + daemon integration); some prior test runs in `test-results/`.

**Binary Naming Note:** Code/Makefile/CLI spec use `aegis` (and `bin/aegis`). `AGENTS.md` and some older text use `aegisclaw`. This is a minor but real inconsistency.

**Maturity:**
- **Good reference:** Component split, sandbox abstraction, partial AegisHub (registration + ACL + messages), orchestrator, Firecracker spawning, message structs everywhere, E2E harness + some journeys, build system.
- **Drift / Incomplete vs Current Specs:**
  - Host Daemon: Skeleton only. Socket is toy (`"vm list"` text), world-readable perms (0666 — violates hardening), stop requires root (violates AGENTS/CLI spec), no key distribution, no audit signing, no watchdog/bootstrap sequence, no containment on crash, no reverse proxy.
  - Web Portal: Demo/mock only. Direct `:8080` listen, fake Ollama streaming (violates isolation/net-boundary), limited screens vs detailed wireframes in `web-portal-screens.md`, no thin "proxy-only" behavior.
  - Most other `cmd/*`: Stubs with Message types + hub connect loops. No real 6-step loop, no 5 gates, no merkle store, no persona voting, etc.
  - No full end-to-end component wiring via vsock/unix + AegisHub ACLs in a running system.
  - Limited journey test coverage (4/9 superficial UI mocks).
  - Some path/perm/naming drifts.

**Overall:** Excellent starting skeleton and architecture. Significant work remains, but the split reduces blast radius.

## 3. Reuse vs. Rewrite / Build-From-Scratch

**High Reuse / Light Adaptation:**
- `internal/sandbox/*` (factory, backends, types, vsock support) — extend for per-component images, key injection, health, robust cleanup.
- `internal/runtime/orchestrator.go` + `VMLifecycle` — extend for bootstrap ordering, key handling, watchdog, metrics.
- `internal/security/manager.go` — base + per-VM keygen/distribution.
- `cmd/aegishub/main.go` — registration, routing, ACL yaml, message types — complete the spec (verify sigs, hot-reload, auditing, deny-by-default).
- Most other `cmd/*/main.go` + `main_test.go` — use as templates (cobra + Message + hub loop); implement domain logic per specs.
- `e2e/` (Playwright config, journeys.spec.js, fixtures, testdata/) + `cmd/web-portal/testdata/` — expand dramatically.
- `scripts/build-microvms-docker.sh` + Makefile targets — enhance for all components + signed? images.
- `docs/` specs, prd, architecture, testing-standards — authoritative.
- Basic Go deps (cobra, logrus, yaml) + stdlib (ed25519, net/http for proxy if needed).

**Significant Rewrite or From-Scratch:**
- **Host Daemon (`cmd/aegis/` + daemon_*.go + socket handling):** Must become the tiny TCB. Rewrite socket protocol (rich, validated, per CLI), bootstrap logic, key distribution, audit signing, reverse proxy (minimal), crash containment, privilege model for stop. Add comprehensive TCB tests.
- **Web Portal (`cmd/web-portal/` entire):** Server must be thin (serve static + forward APIs/SSE/WS to AegisHub, health, no business logic/state/secrets/LLM). Frontend must realize **all** screens/wireframes from `web-portal-screens.md` (Dashboard, Chat streaming, Teams, Court, Skills, Monitoring, Proposals, etc.) with exact palette, `data-testid`s everywhere, self-contained. Run inside its own sandbox.
- **Domain Logic in Sandboxes:** 6-step agent loop, Court personas (7), Scribe summaries, Builder 5 gates (SAST/SCA/secrets/policy/composition — need scanners in rootfs), Store (persistence + Merkle), Memory, Network Boundary (policy + secrets), Secrets vault, semantic discovery, etc.
- **Full CLI Surface (`aegis chat`, `sessions`, `tasks`, `skills propose`, Court commands, autonomy, etc.):** Beyond current daemon mgmt commands.
- **Integration & Bootstrap:** Daemon starting the full core set (Hub, Store, NetBoundary, WebPortal, Court, etc.) with ACLs, keys, vsock wiring.
- **Test Suite:** All 9 journeys with real/near-real automated tests; 80%+ coverage; chaos; security gates; containment proofs.
- **Consistency/Polish:** Naming (recommend standardize on `aegis` per CLI spec/Makefile; update AGENTS.md), paths, socket hardening, docs links (e.g. missing web-portal.md reference), rootfs per-component.

## 4. Risk Areas (Especially TCB & Security Boundaries)

1. **Host Daemon TCB Bloat & Scope Creep (Critical — Highest Priority Mitigation):** Target <2000 LOC total for the privileged binary. Reverse proxy for web-portal (required by `web-portal-vm.md`) adds HTTP surface. **Mitigation:** Ruthless reviews against the 5 responsibilities in `host-daemon.md`; use stdlib `net/http/httputil.ReverseProxy` with tight limits (size, rate, timeouts); consider tiny sidecar if needed but prefer in-process minimal; heavy static analysis + tests. Never let business logic creep in.
2. **Secure Per-VM Key Distribution & "Private Key Never Leaves VM":** How daemon gives Ed25519 privkey to its assigned VM only (and forgets it). **Mitigation:** Options — ephemeral overlay fs written then ro-mounted, one-time vsock handshake at init (daemon sends then zeroizes), or key gen inside VM with pubkey returned. Prove via tests + code inspection that daemon never retains or logs privkeys post-distribution.
3. **Lifecycle Containment on Crash (Spec Requirement):** "If the daemon crashes, all running microVMs must be terminated." Current uses process.Kill + backend.Cleanup on graceful exit. **Mitigation:** Use Firecracker jailer + cgroups/namespaces owned by daemon; reparent/orphan reaping; explicit test (kill -9 daemon, assert zero remaining fc/Docker sbx processes for AegisClaw).
4. **Unix Socket Hardening & Auth:** Currently 0666 world-readable + trivial text protocol. **Mitigation:** 0600 or user-only + uid check; structured + signed/validated commands; input length/allowlist; tests for the hardening reqs in `host-daemon.md`.
5. **Web Portal Inbound Proxy & Isolation:** Browser traffic must be mediated. If in TCB, surface risk. Also ensure web-portal VM has zero direct net (only via Hub).
6. **Cross-Platform Parity & Firecracker Realities:** KVM/`/dev/kvm`, jailer, rootfs paths (`/opt` fallback `~/.aegis`), Docker sbx emulation of vsock. **Mitigation:** Backend abstraction already good; test both paths; doctor checks.
7. **Rootfs / Component Image Security & Reproducibility:** Per-component images (include the right `cmd/xxx` binary + minimal userspace). Secrets/keys never baked in. **Mitigation:** Enhance build script; sign images; runtime injection only.
8. **Journey Test Completeness & Complexity:** 9 journeys include multi-agent, full SDLC with Court + Builder gates, autonomy changes. Hard to automate fully without good harness. **Mitigation:** Define success criteria from each journey spec; build test helpers early; do not mark "done" until automated and green.
9. **Spec Drift During Implementation:** Easy to implement "what the code suggests" instead of current specs. **Mitigation:** Every task references specific spec sections; plan phases tie back; use reviewers.
10. **Performance/UX Overhead of Many Sandboxes:** Acceptable per architecture trade-off, but monitor.

## 5. Dependencies & Ordering

- **Foundational (blocks everything):** Phase 1 (TCB/Hub bootstrap, keys, basic launch, containment, hardened socket).
- **Communication Core:** AegisHub full + Store + Network Boundary (enables any inter-VM work).
- **Execution Core:** Agent Runtime (6-step) + Memory + Court Scribe/Personas (parallelizable after Hub+Store).
- **Extension:** Builder + Gates (for skill journeys 4 & 9).
- **Presentation:** Web Portal rewrite (unblocks rich UI journeys; CLI can cover some flows earlier).
- **Closure:** All 9 journey tests + CLI richness + gaps + final validation.
- **Parallelism:** After Phase 1-2 skeleton, multiple component teams (or subagents) can implement in parallel against the hub contract. Web UI can be developed against mock hub early.

Old 25-phase plan provides ordering hints but is superseded by the component split and current specs.

## 6. Complete Phased Plan

### Phase 0: Alignment, Naming, AGENTS Compliance, Quick Wins (1-2 days)
- Standardize binary name to `aegis` (per `docs/specs/cli.md`, Makefile, most code). Update `AGENTS.md` references from `aegisclaw` to `aegis` (or `bin/aegis` / `make start`) while preserving the exact sudo/no-sudo and script guidance.
- Fix `stopDaemon`: Make it work without root per AGENTS.md + CLI spec (e.g., graceful socket-based stop command + fallback; or relax the uid check and ensure perms allow signal from original user). Update tests.
- Fix socket permissions (0666 → 0600 or 0660 with group) + basic validation in `handleSocketCommand`.
- Ensure `make start / make stop / make doctor` exactly match AGENTS.md behavior and work on the branch.
- Add `data-testid` baseline to existing web-portal static (prep for E2E expansion).
- Read all 9 journey specs + key component specs end-to-end (team exercise).
- **Deliverable:** Clean, consistent foundation. No security behavior change yet. All `make test` + existing E2E still pass.
- **Tests:** Update daemon integration tests for stop/privs/socket.

### Phase 1: Host Daemon TCB — Minimal, Hardened, Functional Bootstrap (Highest Priority, Foundational)
**Goal:** Daemon matches `docs/specs/host-daemon.md` exactly (5 responsibilities only; explicit non-resp never violated). <2000 LOC. Keys, audit, containment, hardened socket/CLI surface.

Sub-steps:
1. Implement full keypair generation + secure distribution logic (extend `security.Manager`; integrate into `Orchestrator.StartVM` and VMConfig; support injection for Firecracker rootfs overlay or vsock init). Add tests proving isolation ("privkey never in daemon after handoff").
2. Add Merkle tree root signing (periodic or on events) by daemon. Basic audit log append in Store later.
3. Core bootstrap sequence on `start`: Launch (in strict order, with health waits): AegisHub, Network Boundary, Store VM, Web Portal VM, Court Scribe, Court Personas (7), base Memory if needed. Dynamically update `config/acls.yaml` + hot-reload Hub. Assign unique IDs + keys.
4. Watchdog: Monitor critical VMs (Hub, Store, NetBoundary, WebPortal); restart or Safe Mode on failure.
5. Lifecycle containment: Robust termination (jailer + cgroups if possible; test `kill -9` on daemon → zero Aegis VMs left). Enhance `orchestrator.Shutdown` + backend.Cleanup.
6. Minimal reverse proxy / inbound HTTP mediation for Web Portal (in daemon or tightly controlled). Expose on localhost:8080 (configurable). Strict limits, logging to audit. Decide minimal implementation (stdlib ReverseProxy with guards recommended).
7. Enrich socket protocol (structured JSON or length-prefixed + signatures/ACLs) for CLI commands per `cli.md`. Support `status`, `vm list`, basic `chat` handoff, etc. Hardened parsing.
8. `aegis doctor` expanded with TCB checks (mem usage, static build, no extra caps, key isolation probe, containment test hook).
9. Static binary + idle mem <20 MB target (measure, strip, optimize).
10. **All tests from host-daemon.md** automated (minimal priv, no secrets ever touches daemon, key isolation, containment, signing, socket hardening, isolation from compromised sandbox).

**Dependencies:** None (uses/extends existing sandbox/orchestrator).  
**Risk Focus:** Items 1-4 above. Code reviews must cite the spec's "never" list.  
**Deliverable:** `sudo make start` brings up core VMs (verifiable via `aegis vm list` + logs). Socket works for non-root. Crash test passes. TCB tests green. Web Portal VM reachable via daemon proxy (even if UI basic).  
**Tests:** New TCB-specific integration tests + existing daemon ones updated.

**Phase 1 Autonomous Progress (this session):**  
- Naming/AGENTS alignment (aegis binary everywhere, AGENTS.md updated to match CLI spec + Makefile + no-sudo stop).  
- stopDaemon root requirement removed; primary path now socket "stop" (non-root works, daemon honors it) + fallback. Compatible with existing integration tests.  
- Socket hardened: 0600 + chown to original user (only that user + root can control), input length/allowlist validation + "unauthorized" rejection + logging. New TestSocketHardening + TestTCBComplianceSkeleton.  
- security.Manager extended with GenerateVMKeyPair (priv never stored in Manager), RegisterVM/Get/List for pubs, used for isolation proof. New manager_test.go with TestGenerateVMKeyPair_Isolation + daemon key regression test.  
- Orchestrator.StartVM now generates per-VM keypair on every launch, populates VMConfig (pub + priv for backend injection), registers pub post-start, zeros local copy. Added SignAuditRoot. Daemon calls it on ready (genesis).  
- All short tests `./...` green post-changes. Full build + CLI smoke (start/stop/doctor/vm list) exercised. Mechanisms for key distrib, audit signing, hardened control, and non-root lifecycle now in place and tested (foundational for all later phases).  
- Some sub-items (full reverse proxy, deep doctor TCB metrics, complete bootstrap list of 10+ components) deferred to keep Phase 1 focused + TCB tiny; proxy strategy documented as risk in plan.  
**Next recommended:** Phase 2 (AegisHub completion + Store) or flesh out the core bootstrap list + real image injection in sandbox backends.

### Phase 2: AegisHub, Store VM, Network Boundary — Communication & Persistence Core
- Complete `cmd/aegishub/`: Signature verification on every message (using per-VM pubkeys registered at handshake), full deny-by-default ACL hot-reload from `config/acls.yaml`, reject+audit unauthorized, support all command categories from specs.
- `cmd/store/`: Persistent ownership (proposals, skill registry, composition history, full Merkle audit log). Tamper-evident appends, query APIs via Hub.
- `cmd/network-boundary/`: Outbound-only proxy (to Ollama, external), secret handling per `secret-management.md` and `network-boundary.md`, policy enforcement.
- `cmd/secrets/`: Vault (if separate per split).
- Basic vsock vs unix abstraction or dev/prod modes.
- ACL updates from Host Daemon during VM lifecycle.

**Dependencies:** Phase 1 (daemon launches them with keys/ACLs).  
**Deliverable:** Components register, signed messages route, ACLs enforced, Store persists, NetBoundary allows controlled egress. Simple end-to-end "hello" via Hub works.  
**Tests:** Integration tests for routing, ACL denies, persistence, signing.

### Phase 3: Agent Runtime, Memory, Court Scribe & Personas — Execution & Governance Core
- `cmd/agent/`: Stateless 6-step loop (Observe → Think → Plan → Act → Execute → Judge) per `agent-runtime.md`. Tool/skill calls exclusively via Hub. Interleaved background tasks. Autonomy modes.
- `cmd/memory/`: Short-term context + long-term per agent (paired with runtimes).
- `cmd/court-scribe/`: Observes, generates structured summaries for Court.
- `cmd/court-persona/`: The 7 personas (CISO, Security Architect, etc.) — review logic, voting (Approve/Reject/Abstain) per governance PRD/specs. Unanimous or threshold rules.
- Court review flows, notifications.

**Dependencies:** Phase 2 (Hub + Store for state/proposals).  
**Deliverable:** Basic agent can run a trivial loop, talk to memory, trigger Court review on a proposal.  
**Tests:** Unit for loop steps + persona voting; integration journey seeds.

**Phase 3 Progress (current session):**  
- `cmd/agent/main.go` already had strong skeleton... Enhanced with distinct 6-step prompts, memory context in Observe, proposal trigger in Judge, callLLMWithFallback + rich persona-aware mocks. Fixed createProposal to notify Scribe with **ID only** (no content leak, per court-scribe.md). Added unit tests (mock, payload security).
- Memory VM: spec-aligned get_context (32k tokens, semantic top-N long-term, token summary), normalizeVector for embeddings, ioutil fixed, persist to Store, expanded tests.
- Court Scribe: full keys+signing+pubkey reg, content guard (rejects desc), forwards notify to 7 unique personas via Hub, real decideReview (unanimous non-abstain Approve or any Reject blocks), signed review_complete. Tests for rules.
- Court Personas: unique sources "court-persona-<name>" (7 distinct, ACL wildcard enabled), persona-specific analysis producing distinguishable votes/reasoning + Abstain on uncertainty, structured feedback, signed Store fetch + vote. Tests.
- ACLs: wildcard support + comprehensive Phase 3 rules (agent/memory/store/net/court flows, bidirectional, deny-default).
- All: builds, relevant tests green (go test ./cmd/{agent,memory,aegishub,court-*}). 3 logical commits. Hub roundtrips pass with DEV_MODE + explicit ACL.
- Remaining (see todos): full E2E flow tests with live hub+components, deeper persona LLM via NetBoundary, Builder integration, plan user journeys seeds.

### Phase 4: Builder VM + Mandatory Security Gates
- `cmd/builder/`: Ephemeral per-proposal. Implements the 5 gates in order (`builder-security-gates.md` + `builder-vm.md`):
  1. SAST
  2. SCA
  3. Secrets/sensitive scan (deliberately vague errors)
  4. Policy-as-Code (Rego or equiv)
  5. Composition/health + smoke + rollback prep.
- Integrate scanners into the component rootfs (enhance build script).
- Gates feed into Court proposals.

**Dependencies:** Phase 3 (proposals from Court/Agent).  
**Deliverable:** Builder runs on proposal, gates pass/fail correctly, artifacts signed.  
**Tests:** Security gate unit + integration tests (malicious skill examples blocked).

**Phase 4 Progress (current):** All 5 mandatory gates (including final SCA enhancement) implemented + hardened with strong regression tests. Full wiring (proposal trigger + sequenced git/PR/skill flow). Rootfs support complete (Builder Dockerfile with the 5 scanners + build script improvements). Pre-Phase 5 stub audit performed: critical Network Boundary hardcoded values hardened with env support + tests; other minor stubs documented. Phase 4 surface clean. 9+ commits. See full-implementation-roadmap.md.

### Phase 5: Web Portal — Full Thin VM + Complete UI Screens

**Post-integration note (from 6f4d470 on update/web-portal-specs-and-api):**  
The authoritative specification is now `docs/specs/web-portal.md` (integrated via cherry-pick). It provides a current implementation review of the rich portal (GitHub-dark theme, Canvas/Chat/Overview/Skills/PRs/Workspace, real-time SSE/streaming chat + tool events, full documented feature set). Related files (web-portal-screens.md, web-portal-vm.md, additional-requirements-and-gaps.md, chat-ui-data-flow.md, issue-35.md) were refreshed. Concrete REST API surface was added in the reference `internal/dashboard/server.go` (brought in as part of the integration):
- `POST /api/proposals` (create)
- `GET /api/proposals/{id}/status`
- `GET /api/proposals/{id}/audit`
- `GET /api/workspace/read`
(and supporting handlers for the E2E SDLC/workspace flows).

Phase 5 work must implement (or adapt) this API contract + the rich UI described in the new primary spec, while staying thin/presentation-only (all actions via Hub/daemon bridge).

- **Server (`cmd/web-portal/main.go` rewrite/adapt or adapt from reference `internal/dashboard/server.go`):** Thin per the new `docs/specs/web-portal.md` + `web-portal-vm.md`: Embed/serve updated static, implement the documented internal + public REST endpoints (proposals, workspace, etc.), SSE/WebSocket for realtime (chat streaming, tool/thought events, /events), validate + forward to AegisHub/Host bridge (no local LLM/state/secrets), `/health`. Graceful degradation. Runs inside its dedicated sandbox VM. Build rootfs with the binary + assets.
- **Frontend (static/ + app.js/css):** Realize **every** screen/wireframe from `web-portal-screens.md` (and proposal detail):
  - Consistent header (status, nav: Dashboard/Conversations/Teams/Agents/Skills/Court/Monitoring/Audit, avatar, notifications, conn).
  - Exact color palette, typography, dark "secure command center".
  - Dashboard (quick actions, active agents, background tasks, system health, safe mode).
  - Chat (streaming incremental Markdown, tool calls visible, context panel).
  - Team workspaces (multi-agent, shared context, timeline).
  - Court / Governance + Proposal detail (votes, diffs, gates status, comments).
  - Skills registry (installed/proposed, details, propose).
  - Monitoring (agents, tasks, logs, safe mode toggle).
  - Others implied (Audit, Settings, Agent Customization).
  - All interactive elements have stable `data-testid` (per testability req).
  - Self-contained (no CDNs). Fast, paranoid UX.
- Host Daemon proxy integration (from Phase 1) so `localhost:8080` reaches the VM safely. **Completed in this session**: minimal hardened ReverseProxy + managed web-portal child process on internal address (stdlib httputil with limits + logging). See phase5-08.
- E2E expansion: Drive the real (or stubbed) flows.

**Dependencies:** Phase 1 (daemon launches + proxies it), Phase 2 (Hub for data). Can prototype UI against mock early.  
**Deliverable:** `make start` → open http://localhost:8080 → full rich UI works for core flows, realtime updates, all major screens. Playwright covers the screens.  
**Tests:** Expanded E2E for UI journeys + accessibility basics.

**Phase 5 Progress (current session):** First major milestone achieved — Web Portal is now strictly thin (detailed in full-implementation-roadmap.md). `cmd/web-portal` reduced to thin entrypoint + bridge client; rich UI comes from the reference implementation. Direct business logic removed.

- Completed implementation of the documented public REST / JSON API surface from `docs/specs/web-portal.md:148-176` (POST /api/proposals returning 201+id; GET /api/proposals, /api/proposals/{id}/status (exact shape), /api/proposals/{id}/audit (md/text); GET /api/skills, /api/approvals; plus recommended /api/court/decisions, /api/prs, /api/build/status). All strictly thin (delegation only via the hubBridgeClient signed Message protocol + APIClient; no local logic/state). Fixed ID generation for proposal.create compatibility with Store. Consistent JSON errors. Expanded tests in cmd/web-portal/main_test.go with delegation assertions proving thinness. Logical commit + tests green. (phase5-11)

- Expanded Playwright E2E + data-testid baseline for the 9 documented user journeys (phase5-09):
  - Added dozens of stable `data-testid` across static/index.html and all major render templates in internal/dashboard/server.go (chat, proposals, approvals, dashboard stats, nav, review grids, decide forms — per web-portal.md Testability section and testing-standards.md).
  - Dramatically expanded e2e/journeys.spec.js to drive UI flows + directly exercise the new public REST endpoints for proposals/status/audit/court/approvals (asserting exact shapes and Success Criteria from every user-journeys/*.md, especially 02/04/05/06/09).
  - Fixed playwright.config.js webServer for reliable thin-portal startup in E2E.
  - Go tests + build green; Playwright structure ready (full live runs with daemon per AGENTS.md will cover the complete Court/Builder SDLC paths).
  - Logical commit + plan updates.

- Created detailed forward plan for the remaining major UI gap: **Teams / Multi-Agent Collaborative Views** (see `docs/implementation-plan/teams-multi-agent-plan.md`). Executed the rich UI slice autonomously: dedicated `/teams` page with create success banners/feedback + "View in Canvas" CTAs, team.message send form + activity counts in table, richer per-team dashboard cards (stats, roles, msgs, links), all thin bridge + data-testid, Canvas `?team=` integration, and plan updates. (See teams plan "Progress Update" for details.) Builds directly on Canvas + thin architecture.

### Phase 6: Full CLI, Complete 9 User Journeys, End-to-End Integration
- Flesh out all CLI commands in `aegis` binary (or thin client) per `cli.md` (chat, sessions, tasks, skills, court, autonomy, etc.). All non-start commands non-root, JSON support, etc.
- For **each of the 9 user journeys** (detailed in `docs/specs/user-journeys/`):
  - Implement the exact step-by-step flows (proposals, Court review, Builder gates, deployment, monitoring, multi-agent, autonomy changes, Discord skill example, etc.).
  - **Automated integration tests** (Go + Playwright where UI involved) that exercise the full path and assert the "Success Criteria (Testable)" sections.
  - Only mark journey complete when tests are green and cover the criteria.
- Wire everything: Agent → Memory ↔ Court Scribe ↔ Personas ↔ Builder ↔ Store ↔ NetBoundary ↔ Web/CLI.
- Safe Mode, observability, event system.
- Expand `aegis doctor`, logs, audit viewing.

**Dependencies:** Phases 1-5.  
**Deliverable:** All 9 journeys executable end-to-end (via CLI or Web Portal) with passing automated tests. System feels cohesive.

### Phase 7: Gaps, Polish, Hardening, Final Validation & Release Prep
- Address `docs/specs/additional-requirements-and-gaps.md`, `ollama-integration.md`, `semantic-tool-discovery.md`, `event-system.md`, `configuration-management.md`, full threat model mitigations.
- 80%+ coverage across the board (report in CI).
- Chaos/failure injection tests (component crashes, recovery).
- Supply-chain: image signing, pinned hashes, SBOM in builder.
- Performance baselines, resource limits per VM.
- Full docs alignment (fix broken links like web-portal.md reference), README, AGENTS.
- Final security audit of TCB + boundaries.
- `make test`, `make test-integration`, `make test-e2e` all green; doctor clean on Linux + Docker platforms.
- Branch ready for review/PR to main.

## 7. Success Criteria for the Whole Effort

- `sudo make start` (per AGENTS) brings up a fully wired, spec-compliant system.
- Web Portal at localhost:8080 matches the design and screens exactly; all data flows through proper boundaries.
- Every user journey 1-9 has automated, passing tests that would catch regression.
- Host Daemon passes its own TCB test battery and stays tiny/minimal.
- No component violates its "non-responsibilities".
- 80%+ coverage + clean `make test*`.
- Developer can follow README/AGENTS and have a working paranoid agent platform locally.

## 8. How to Work (Per AGENTS & Project Norms)

- Follow `AGENTS.md` **exactly** for starting/stopping.
- One small focused task at a time (old impl-plan style).
- Reference the specific spec section in every commit/PR.
- Use `todo_write` for multi-step work.
- Never declare a journey/feature done without its automated tests.
- When in doubt on TCB surface area, push back and cite `host-daemon.md`.

---

**This plan is now the guide.** It distinguishes reuse, flags risks (especially TCB), prioritizes security foundations, ties everything to current specs and the mandatory testing standard, and respects the component architecture proven on this branch.

I have completed the deep exploration of the codebase (cmd/ split, internals, build, tests, e2e) and all referenced specifications.

**Which phase or specific component (e.g., Phase 1 TCB work, Host Daemon socket/hardening, Web Portal rewrite, a particular journey's tests, AegisHub completion, etc.) would you like to start with?**

I am ready to:
- Generate a detailed sub-plan for that phase/component.
- Break it into actionable todos.
- Begin coding, tests, or further targeted exploration.

Just say the word (e.g., "Start Phase 1" or "Detail the key distribution sub-plan for the daemon" or "Focus on fixing the web portal to match screens.md first").