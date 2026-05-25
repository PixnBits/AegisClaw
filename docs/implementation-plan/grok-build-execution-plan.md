# Grok Build Execution Plan – AegisClaw v2

**Branch:** `docs/lessons-learned`  
**Version:** 2026-05-24  
**Purpose:** Single source of truth for fully autonomous Grok Build sessions with minimal user input.

---

## 1. Grok Build Operating Principles (Non-Negotiable)

1. **Follow the specs exactly** — Every change must reference the precise section in `docs/prd/`, `docs/specs/`, or `docs/implementation-plan/`.
2. **Security-First (Paranoid Mode)**:
   - `cmd/aegis` (Host Daemon) is the **only** privileged component and the minimal Trusted Computing Base (TCB).
   - All inbound traffic must go through the hardened reverse proxy.
   - No direct LLM calls outside sandboxed VMs.
   - Every privileged action must be auditable (Merkle root signing where required).
   - Strict least-privilege, 0600 socket permissions, input validation, crash containment, and key isolation.
3. **Definition of Done** for every task:
   - Compiles cleanly + `go test ./...` passes.
   - New/changed code has ≥80% unit test coverage.
   - Relevant E2E (Playwright) test passes and asserts Success Criteria from the spec.
   - Status in this plan and `00-v2-phased-implementation-plan.md` is updated.
4. **Commit Discipline**:
   - One logical change per commit.
   - Message format: `phaseX: short description (refs spec/xxx.md:section)`.
   - Never break `make test`, `make start`, or `make stop`.
5. **Autonomy Rule**: When blocked, document the exact issue in this plan (or a new TODO entry) and move to the next task. Do **not** wait for user input unless the blocker is in the specs themselves.
6. **Hardware Context (Framework Desktop 128 GB, Ubuntu 26 server)**:
   - Use Firecracker microVMs for all Linux sandboxes (performance).
   - Ollama is available locally for development.
   - Leverage high RAM for parallel builds and test runs.
   - Docker is **not** used for sandboxes on this Linux setup.

**Recommended Session Prompt Prefix**:
> "Follow the Grok Build Execution Plan (docs/implementation-plan/grok-build-execution-plan.md) exactly. Work completely autonomously. Update task status after each major item. Prioritize security and test coverage."

---

## 2. Current State Snapshot (2026-05-24)

- **Strongly Completed**: Phases 0–5 (component-split architecture, hardened Host Daemon TCB + reverse proxy, Builder VM with 5 security gates, Court + 7 personas + voting, Agent 6-step loop, Memory VM, thin Web Portal + Teams views + Audit explorer).
- **Remaining Major Work**:
  - Phase 6: Complete CLI surface + **all 9 User Journeys** with automated E2E tests.
  - Phase 7: Final gaps, 80%+ coverage, supply-chain hardening, polish.
- **Known Gaps** (from `docs/specs/additional-requirements-and-gaps.md`): CLI completeness, full journey automation, Network Boundary VM, EventBus, semantic tool discovery, workspace customization (`~/.aegis/` files), remaining Host Daemon watchdog/key distribution.

---

## 3. Prioritized Task List

### Phase 6: Full CLI + All 9 User Journeys (Highest Priority – "Feature Complete" Milestone)

**Goal**: Every user journey in `docs/specs/user-journeys/` has a working end-to-end flow + automated Playwright test asserting all Success Criteria.

**Task 6.1 – Complete CLI Surface** (Foundation for all journeys)
- Implement all missing commands per `docs/specs/cli.md` and `additional-requirements-and-gaps.md`:
  - `aegis restart`, `aegis team *`, `aegis skills status`, `aegis court decisions show`, session/task control verbs, `aegis autonomy grant/revoke/reset`, `aegis audit verify`, full secrets lifecycle (`set/list/remove`).
- Add proper `--help`, JSON output options, and robust error handling.
- **Security Requirement**: All privileged CLI actions must route through the daemon socket (no direct root bypass).
- **Acceptance Criteria**: `aegis --help` shows complete command tree; every command has unit tests + passes `make smoke`.
- **Order**: Complete this task first — it unblocks all subsequent work.

**Task 6.2 – User Journey 01: Installation & Onboarding**
- File: `docs/specs/user-journeys/01-installation-onboarding.md`
- Implement full flow + automated E2E test asserting **all** Success Criteria (fresh install, daemon start, `aegis doctor`, initial key setup).

**Task 6.3 – User Journey 02: Starting a New Conversation**
- File: `docs/specs/user-journeys/02-starting-new-conversation.md`
- Full E2E test with chat streaming through thin portal + AegisHub.

**Task 6.4 – User Journey 04: Creating & Iterating a New Skill**
- File: `docs/specs/user-journeys/04-creating-iterating-new-skill.md`
- End-to-end Builder VM flow including all 5 security gates, SBOM generation, and signed artifact.

**Task 6.5 – User Journeys 03, 05, 06, 07** (Recommended order)
- 03: Collaborative Task Execution (`03-collaborative-task-execution.md`)
- 05: Monitoring Agent Activity (Audit explorer) (`05-monitoring-agent-activity.md`)
- 06: Reviewing Court Decisions (`06-reviewing-court-decisions.md`)
- 07: Granting/Adjusting Autonomy (`07-granting-adjusting-autonomy.md`)
- Full E2E + assertions for each.

**Task 6.6 – User Journeys 08 & 09**
- 08: Multi-agent Team Workflows (`08-multi-agent-team-workflows.md`)
- 09: Adding Discord Monitor Skill (`09-adding-discord-monitor-skill.md`)
- Complete team creation, messaging, activity feed, and external skill integration with E2E tests.

**Task 6.7 – Journey Test Suite Hardening**
- Ensure all 9 journeys run reliably in both **fixture mode** and **live daemon mode**.
- Add visual regression support + Git LFS for screenshots (per `TESTING.md`).
- Make `make test-e2e` consistently green.

---

### Phase 7: Gaps, Polish, Hardening & Release Prep

**Task 7.1 – Network Boundary VM (Full Implementation)**
- Per `docs/specs/network-boundary.md`: full Envoy configuration, secret injection, outbound proxy rules, and crash containment.
- **Paranoid Security**: Zero-trust outbound traffic, strict allowlists only, comprehensive egress audit logging.

**Task 7.2 – EventBus & Background Services**
- Implement internal EventBus per `additional-requirements-and-gaps.md`.
- Support timers, signals, approval queues, and scheduled background tasks.

**Task 7.3 – Semantic Tool/Skill Discovery**
- Runtime `list_skills()` + semantic search available in every Agent VM (fast local index, always up-to-date).

**Task 7.4 – Workspace Customization**
- Support loading and precedence rules for `~/.aegis/{AGENTS.md, SOUL.md, TOOLS.md, SKILL.md}` with proper validation and security checks.

**Task 7.5 – Host Daemon TCB Completion (Remaining Items)**
- Full watchdog + automatic crash containment (jailer/cgroups + restart policy).
- Complete secure key distribution to all VMs at bootstrap.
- Expanded `aegis doctor` with deep health metrics and Merkle root verification.
- Static binary verification on daemon startup.

**Task 7.6 – Supply-Chain & Release Hardening**
- Image signing (cosign or equivalent) in build pipeline.
- Automated SBOM (CycloneDX) generation.
- Achieve ≥80% overall test coverage across the codebase.
- Add chaos tests for daemon crash/restart scenarios.

**Task 7.7 – Final Polish & Documentation Sync**
- Update all cross-references in PRD and specs.
- Final review pass on `AGENTS.md`, `CONTRIBUTING.md`, `README.md`.
- Create release notes template and changelog.

---

## 4. Recommended Execution Order (Minimal Context Switching)

1. **Task 6.1** (Complete CLI) — unblocks everything else
2. **Tasks 6.2 → 6.7** (All 9 User Journeys in priority order)
3. **Tasks 7.1 → 7.5** (Core remaining gaps + TCB completion)
4. **Tasks 7.6 → 7.7** (Final hardening and polish)

---

## 5. How to Use This Plan with Grok Build

**Best Practice for Every Session**:
- Start with the full plan content + the exact task number(s) you want to work on.
- After completing a task (or logical group), **update this file** with status:
  - ✅ Done
  - 🔄 In Progress
  - ⏳ Blocked (with clear reason)
- When you want a progress report, simply say: "Give me current status against the Grok Build Execution Plan."

**Autonomous Mode Enabled**: This plan + the Operating Principles above are designed so Grok Build can work for long stretches with almost no additional context from the user.

---

**Status Legend** (update as you go):
- ✅ = Complete
- 🔄 = In Progress
- ⏳ = Blocked

**Current Overall Progress**: Phase 6 in progress (CLI + Journeys remaining)
