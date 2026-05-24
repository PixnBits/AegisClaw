# AegisClaw Full Implementation Roadmap

**Branch:** `docs/lessons-learned`  
**Date:** 2026 (post-Phase 3 progress)  
**Status:** Authoritative actionable execution plan  
**Primary Sources:** All `docs/prd/` and `docs/specs/` (especially the 9 user journeys' "Success Criteria (Testable)" sections, component specs, security-model.md, threat-model.md, testing-standards.md, architecture.md, and web-portal.md).  
**Relationship to Existing Plan:** This document refines and operationalizes `00-v2-phased-implementation-plan.md`. The v2 phased plan remains the high-level reference; this provides granular tasks, traceability, and updated status after Phase 3 work.

## 1. Guiding Principles (Non-Negotiable)

Drawn directly from PRDs and Specs:

- **Paranoid by Design / Every Boundary is a Security Boundary** (`security-model.md`, `architecture.md`, `threat-model.md`).
- **Minimal TCB**: Host Daemon has *only* 5 responsibilities (`host-daemon.md`, `runtime-architecture.md`). Explicit non-responsibilities are enforced.
- **Mandatory Governance for *Every* Change**: No code, prompt, skill, or autonomy change ships without Court review + user veto (`governance-court.md`, `sdlc-governance.md`, `prd/governance-court.md`).
- **Secret Isolation**: Network Boundary VM is the *only* place secrets may exist (`secret-management.md`, `network-boundary.md`).
- **Test-First Definition of Done** (`testing-standards.md` + every user journey):
  - ≥80% unit coverage for new code.
  - **Every one of the 9 user journeys must have passing automated integration/E2E tests** that assert its "Success Criteria (Testable)" section before the journey (or feature) is considered complete.
- **Local-first + Trust Earned**: Agents start at Level 0; autonomy increases only via Court + user approval (`agent-autonomy.md`).
- **Full Auditability**: Tamper-evident Merkle log for everything (`store-vm.md`).
- **Thin Presentation Layer**: Web Portal is strictly presentation-only (`web-portal-vm.md`, `web-portal.md`).

All work must reference specific sections of the PRDs/Specs.

## 2. Current State Snapshot (as of latest Phase 3 work)

**Strong Foundation (largely complete or significantly advanced):**
- Phase 1: Host Daemon TCB foundations (key distribution skeleton, hardened socket progress, non-root stop, audit signing, TCB tests).
- Phase 2: AegisHub (strict signature verification, hot-reload ACLs, robust registration with pubkeys, deny-by-default).
- Store VM: Signs responses + maintains persistent Merkle audit log for state-changing ops.
- Web Portal specs + concrete API surface integrated (`docs/specs/web-portal.md` + `internal/dashboard/server.go` reference with REST endpoints for proposals/workspace).
- **Phase 3 (recent major progress)**:
  - Agent Runtime: Enhanced 6-step loop (distinct Observe/Think/Plan/Act/Execute/Judge with memory context, improved mocks, proposal creation).
  - Memory VM: Spec-aligned `get_context` (short-term + semantic long-term retrieval, 32k token enforcement, token summary), embedding normalization, persistence to Store, deprecation fixes, expanded tests.
  - Court Scribe + 7 Personas: Keys + signing + pubkey registration, content guard (scribe never receives proposal text), real forwarding to unique `court-persona-*` sources, `decideReview` enforcing exact voting rules (unanimous non-abstain Approve or any Reject blocks), persona-specific distinguishable analysis with Abstain encouragement, new unit tests for rules and behavior.
  - ACLs: Wildcard support (`court-persona-*`, `memory.*` etc.) + comprehensive Phase 3 rules.
  - Multiple logical commits + plan updates.

**Major Remaining Gaps (traced to specs):**
- Full Host Daemon bootstrap, key distribution to *all* VMs, watchdog, crash containment, reverse proxy for web-portal, complete `aegis doctor` TCB checks (`host-daemon.md`).
- Network Boundary VM (real Envoy + per-declaration secret injection + enforcement + crash = block all outbound) (`network-boundary.md`, `secret-management.md`).
- Builder VM + 5 mandatory gates (real SAST/SCA/secrets with deliberately vague errors + Policy-as-Code + composition/rollback; scanners in rootfs; signed artifacts) (`builder-security-gates.md`, `builder-vm.md`).
- Complete, thin Web Portal implementation matching `web-portal.md` + all screens from `web-portal-screens.md` + `data-testid`s + Playwright coverage.
- Full CLI surface per `cli.md` (autonomy, teams, court, audit verify, etc.).
- **All 9 User Journeys** with automated tests asserting their exact Success Criteria (`user-journeys/*.md` + `testing-standards.md`).
- Event system, semantic tool discovery, Safe Mode, observability, configuration management (`event-system.md`, `semantic-tool-discovery.md`, etc.).
- Supply-chain hardening (signed rootfs/images, SBOM in Builder) (`threat-model.md`).
- 80%+ coverage + chaos/failure injection tests across the board.

**Spec Ambiguities / Drifts to Resolve Explicitly:**
- "Five personas" (some PRDs) vs. "Seven personas" (specs + current implementation).
- Exact scope of Court Scribe (conversation summaries vs. proposal coordination).
- Web Portal navigation and exact screen set vs. legacy wireframes.
- Precise autonomy graduation criteria and `soul.md` handling.

## 3. Refined Phased Roadmap (Updated for Execution)

Builds directly on `00-v2-phased-implementation-plan.md` Phases 4–7, with more granular sub-tasks and explicit traceability.

### Phase 4: Builder VM + Mandatory Security Gates (Foundation for all SDLC journeys)

**Dependencies:** Phase 3 (proposals + Court flow working).

**Key Deliverables & Traceability:**
- `cmd/builder/` implements the exact 5 gates in order (`builder-security-gates.md`):
  1. SAST (language-specific patterns, unsafe practices).
  2. SCA (known vulns + license policy).
  3. Secrets/sensitive scan (high-entropy + patterns; **deliberately vague** errors — no details leaked).
  4. Policy-as-Code (Rego or equivalent; e.g., "must route all outbound via Network Boundary").
  5. Composition/health + smoke tests + atomic rollback prep.
- Scanners integrated into component rootfs (enhance `scripts/build-microvms-docker.sh`).
- Builder is ephemeral per-proposal; produces signed artifacts + SBOM.
- Gates feed Court proposals; failures produce non-leaking reports.
- Unit + malicious-skill integration tests (`testing-standards.md`).

**Tasks:**
- Implement real gate runners + integration with Store (fetch proposal) and Court (report results).
- Rootfs updates + build script changes.
- Security gate tests that would catch real malicious examples.
- Update relevant journeys (#4, #9) tests once ready.

**Exit Criteria:** A proposal can go through Builder; gates correctly pass/fail with proper audit; artifacts signed.

**Phase 4 Progress (current):** 
- Core 5 gates implemented and hardened.
- Strong regression test suite for the gates (individual + combined, explicit vague secrets message verification).
- Improved wiring: Builder now properly sends signed requests for git.clone, git.push (gates enforced before any push), and pr.create to Store with response handling (per builder-vm.md).
- Proposal trigger flow: Store notifies Builder after Court approval (signed); Builder fetches proposal, runs gates, reports success/failure back (non-leaking reports).
- Enhanced build_proposal handler now fully sequences the git/PR/skill registration flow: clone → push (gates) → pr.create → skill.register (all signed).
- ACLs updated.
- 6 logical commits in Phase 4.
- Rootfs: cmd/builder/Dockerfile created with minimal alpine + the 5 gate scanners (gosec, govulncheck, opa, gitleaks). build-microvms-docker.sh enhanced with Builder-specific SBOM stub, per-component isolation fix, and correct build context.
- SCA gate significantly enhanced with real vuln/license policy logic + integration notes for the rootfs scanners.
- All 5 gates now have solid implementations and strong regression tests.
- Phase 4 (Builder + 5 Gates) largely complete. Ready for Phase 5.

**Pre-Phase 5 Stub Review (this session):**
- Performed systematic search for remaining stubs/mocks across Builder, Store, Network Boundary, Hub, Web Portal reference, etc.
- Addressed highest-impact items before Phase 5:
  - Network Boundary: Replaced hardcoded allowed domains + secret injection with env-configurable versions + tests (major reduction in stubs for a core security boundary).
  - Store git.push: Documented as remaining simplification (acceptable until real git content wiring).
  - Minor Builder wiring artifacts and Hub ACL TODO noted as low-risk.
- Web Portal internal/dashboard still contains some audit "stub" comments (explicitly per web-portal.md design) — will be cleaned as part of Phase 5 thin-proxy work.
- Agent mocks and Court persona mocks are intentional dev fallbacks (well documented).
- Overall: Phase 4 surface is now much cleaner. No blocking "not yet implemented" paths in core flows.

### Phase 5: Web Portal — Thin VM + Complete UI + All Screens

**Dependencies:** Phase 1 (daemon proxy), Phase 2 (Hub data), Phase 3 (Court/Agent data flows).

**Strict Requirements (`web-portal.md`, `web-portal-vm.md`):**
- Presentation-only: no business logic, no local state/secrets/LLM.
- Realize **every** screen from `web-portal-screens.md` + proposal detail (Dashboard, Chat with streaming + tool/thought events, Teams/Canvas, Court/Approvals, Skills/Proposals, Monitoring, Audit, Workspace/Source/Git/PRs, Settings, etc.).
- Exact GitHub-dark theme, self-contained (no CDNs), stable `data-testid` everywhere.
- SSE/WebSocket realtime (`/events`, chat streaming deltas, tool events).
- Concrete REST surface from the integrated spec (`/api/proposals`, workspace, audit, etc.).
- All actions validated + forwarded via Hub/daemon bridge.
- Playwright E2E coverage for UI journeys.

**Tasks:**
- Ensure/adapt `cmd/web-portal/` + `internal/dashboard/` to be strictly thin per the spec.
- Complete missing screens + interactions.
- Host Daemon reverse proxy integration (minimal, hardened). **Completed**: phase5-08 — daemon now manages web-portal on internal port and exposes hardened ReverseProxy on :8080 (the only allowed inbound path per web-portal-vm.md).
- Full E2E Playwright expansion.

**Exit Criteria:** `make start` → rich UI at localhost:8080 works for core flows; matches design; all major screens present with realtime; Playwright green for UI journeys.

**Phase 5 Progress (current session):**
- First major milestone: Web Portal is now strictly thin (commit 0c21f68).
  - `cmd/web-portal` is a minimal entrypoint + `hubBridgeClient`.
  - All rich UI comes from `internal/dashboard.Server` (the reference implementation).
  - Direct business logic (Ollama, local files, etc.) removed.
- This directly implements the "presentation-only" rule from the specs.
- Completed documented public REST endpoints (phase5-11, commit 5fe97b7): full contract from web-portal.md §"Public REST / JSON API Surface" (POST/GET /api/proposals + variants, status/audit shapes, skills/approvals, plus recommended court/prs/build status). All thin delegation only; ID gen fix for compatibility; JSON errors; tests with call recording proving no local logic. Tests green.
- Expanded Playwright E2E coverage for all 9 documented journeys (phase5-09, commit 469b1f6): comprehensive data-testid sweep (chat, proposals, approvals, stats, nav across templates + static), major journeys.spec.js expansion asserting Success Criteria (Testable) + exercising the new thin REST surface via page.request + UI. Playwright config hardened. Ready for live daemon runs (AGENTS.md).

### Phase 6: Full CLI + Complete 9 User Journeys + End-to-End Wiring

**Dependencies:** Phases 1–5.

This is the primary "done" milestone per `testing-standards.md`.

**Core Work:**
- Flesh out **all** CLI commands in `aegis` per `cli.md` (chat, sessions, tasks, skills propose/list/status, court commands, autonomy grant/revoke, audit verify/log, vm list, doctor, teams, etc.). Non-start commands never require root. `--json` + `--headless` everywhere.
- For **each of the 9 user journeys** (`docs/specs/user-journeys/`):
  - Implement exact step-by-step flows.
  - Write (or complete) automated integration tests (Go + Playwright) that assert **every** item in the journey's "Success Criteria (Testable)" section.
  - Only mark a journey complete when its tests are green and cover the criteria.
- Full wiring: Agent ↔ Memory ↔ Court Scribe/Personas ↔ Builder ↔ Store ↔ Network Boundary ↔ Web/CLI (via Hub).
- Safe Mode, event system, observability, `aegis doctor` expansion, audit viewing.
- Cross-platform parity (Linux Firecracker primary + Docker sbx for macOS/Windows).

**Journey Completion Order Recommendation (dependencies):**
1. 01 Installation/Onboarding + 02 Starting New Conversation (foundational).
2. 04 Creating/Iterating New Skill (drives Court + Builder).
3. 06 Reviewing Court Decisions (audit + transparency).
4. 05 Monitoring Agent Activity.
5. 03 Collaborative Task Execution.
6. 07 Granting/Adjusting Autonomy.
7. 09 Adding Discord Monitor Skill (concrete end-to-end example from PRDs).
8. 08 Multi-Agent Team Workflows.
9. Remaining polish + any gaps.

**Exit Criteria:** All 9 journeys have passing automated tests. User can execute core flows end-to-end via CLI or Web Portal with cryptographic confidence. System feels cohesive and paranoid.

### Phase 7: Gaps, Polish, Hardening, Validation & Release Prep

- Address everything in `docs/specs/additional-requirements-and-gaps.md`, `ollama-integration.md`, `semantic-tool-discovery.md`, `event-system.md`, `configuration-management.md`.
- Full threat model mitigations + final security review/audit of TCB + boundaries (`threat-model.md`).
- 80%+ coverage (CI report) + chaos/failure injection (component crashes, recovery, Safe Mode).
- Supply chain: image signing, pinned hashes, SBOM generation in Builder.
- Performance baselines + per-VM resource limits.
- Full docs alignment, README, AGENTS.md consistency.
- `make test`, `make test-integration`, `make test-e2e` all green on Linux + Docker platforms.
- `aegis doctor` clean.
- Branch ready for review/PR to main.

## 4. Cross-Cutting Requirements

- **Commit Discipline**: Logical commits between sections (as done in Phase 3). Never let work pile up.
- **Testing**: Unit tests written alongside code. Journey tests drive "done". Add tests that would catch security violations (e.g., scribe receiving content, secret leakage).
- **Spec Traceability**: Every significant change should reference the specific PRD/Spec section or journey success criterion.
- **Security Reviews**: Any change touching boundaries, secrets, or the TCB requires explicit review against `security-model.md` + `threat-model.md`.
- **AGENTS.md Compliance**: Daemon start/stop always follows the documented `sudo ./bin/aegis start ...` / `./bin/aegis stop` pattern.

## 5. Risks & Mitigations

- **TCB Bloat** (daemon reverse proxy + full bootstrap): Ruthless scope control; minimal stdlib ReverseProxy with tight limits; heavy testing.
- **Journey Test Complexity**: Start with CLI-driven integration tests; layer Playwright on top. Use fixtures + simulation for Court/Builder early, then real components.
- **Performance of Many Sandboxes**: Accept the trade-off per architecture; measure and optimize only after correctness.
- **Spec Drift**: Re-read relevant PRD/Spec sections before coding any component.
- **Persona/Scribe Subtleties**: Lessons from Phase 3 (unique sources, content guard, voting rules) must be preserved and extended.

## 6. Overall Success Criteria

- `sudo make start` (per AGENTS.md) brings up a fully wired, spec-compliant system.
- Web Portal at localhost:8080 is rich, realtime, self-contained, and strictly thin.
- Every user journey 1–9 has automated, passing tests that assert its Success Criteria and would catch regressions.
- Host Daemon is tiny, passes its TCB test battery, and adheres to its non-responsibilities.
- No component violates its documented responsibilities or security guarantees.
- 80%+ coverage + clean test suite.
- Cryptographic confidence in the audit log and governance process.

## 7. Recommended Next Immediate Steps (Post-This-Plan Approval)

1. User reviews/approves/refines this roadmap.
2. Prioritize Phase 4 (Builder gates) — unblocks real SDLC journeys.
3. Flesh out remaining daemon bootstrap + Network Boundary (enables safe secret/tool use).
4. Tackle journeys in dependency order, writing tests against their Success Criteria as the primary artifact.
5. Continuous spec re-reading + small commits + test runs.

---

This roadmap ensures we build **exactly** what the PRDs and Specs demand, with the 9 user journeys as the ultimate measure of completion. It builds directly on the excellent component-split architecture and the strong Phase 1–3 foundation already in place.

**References for every task:** Read the relevant `docs/prd/*.md` and `docs/specs/*.md` (especially the journey files) before starting work on any area. When in doubt, the specs are authoritative.