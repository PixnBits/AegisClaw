# No-Stubs-Left Resolution Plan – AegisClaw v2.1+

**Status:** Active  
**Branch:** `docs/lessons-learned`  
**Goal:** Eliminate all remaining stubs and fully realize the project per `docs/prd/` and `docs/specs/`.

---

## 1. Current State Summary (May 2026)

The `docs/lessons-learned` branch has reached **v2 surface-complete** status with excellent quality in:

- Host Daemon TCB (7.5)
- Network Boundary (stub-complete with Hub secrets, zero-trust egress, mutual auth)
- EventBus + reconciliation (moved to Store VM)
- All 9 User Journeys (surface + E2E skeleton + recovery tests)
- CLI surface, Web Portal (limited mode), Builder gates, Court simulation
- Supply-chain (SBOM + signing hooks)

**Remaining Work:** Core runtime components that are still surface-only or stubbed.

**Honest Assessment:** ~65–70% of the full specification is production-quality. The remaining 30–35% consists of well-documented stubs that must be replaced with real implementations.

---

## 2. Active Remaining Phases (No Completed Phases Included)

We have **removed** phases that are already substantially complete (Host Daemon TCB, Network Boundary 7.1, EventBus surface, SBOM, 9 Journeys surface) to avoid distracting Grok Build.

### Phase 1: Core Runtime (Highest Priority)
**Goal:** Real Agent Runtime + Memory VM with full 6-step loop.

**Key Specs:** `agent-runtime.md`, `memory-vm.md`, `runtime-architecture.md`

**Definition of Done:**
- Agent can execute the full Observe → Think → Plan → Act → Execute → Judge loop
- Real vsock communication to AegisHub
- Memory VM integration for conversation context + ACLs
- No surface-only disclaimers in agent execution path

**Priority:** P0 – Unblocks almost everything else.

### Phase 2: Store VM Persistent State & Timers
**Goal:** Move all persistent timers, autonomy grants, and background work reconciliation into the Store VM as the single source of truth.

**Key Specs:** `store-vm.md`, `event-system.md`

**Definition of Done:**
- `reconcile.expired_grants` command fully implemented in Store
- Durable storage for autonomy/background state
- No thin wrappers remaining in `cmd/aegis`
- Timers survive daemon restarts

**Priority:** P0 – Directly follows Phase 1.

### Phase 3: Full Court + Governance Runtime
**Goal:** Real 7-persona Court with voting, decision recording, and feedback into Agent Runtime.

**Key Specs:** `governance-court.md`, `court-scribe.md`, `governance-court.md` (PRD)

**Definition of Done:**
- Court personas run as real microVMs
- Voting produces tamper-evident decisions
- Court decisions affect running agents (revoke scopes, etc.)
- Scribe records full audit trail

**Priority:** P1

### Phase 4: Real Encrypted Secrets + Production Network Boundary
**Goal:** Encrypted secret blobs delivered from Store VM + proper zeroization + full vsock guest client.

**Key Specs:** `secret-management.md`, `network-boundary.md`

**Definition of Done:**
- Store VM can push encrypted secrets to Boundary
- Boundary decrypts, injects, and zeroizes after use
- Guest vsock client implemented in Firecracker images
- No file/dir/env fallback in production path

**Priority:** P1

### Phase 5: Complete Web Portal + Full E2E + Final Polish
**Goal:** All Web Portal features wired, 100% E2E automation for 9 journeys, zero remaining stubs.

**Key Specs:** `web-portal.md`, `web-portal-screens.md`, `testing-standards.md`

**Definition of Done:**
- Canvas, full streaming chat with Markdown, proposal detail with round feedback, memory search, approvals all functional
- All 9 journeys have complete E2E automation (including failure/recovery)
- `additional-requirements-and-gaps.md` shows zero open stubs
- ≥85% overall test coverage

**Priority:** P2 (final phase)

---

## 3. Execution Principles

- **No surface-only code** — every feature must have a real backend path.
- **Spec-first** — every change must reference the exact section in `docs/specs/` or `docs/prd/`.
- **Paranoid security** — maintain fail-closed, least-privilege, auditability at every step.
- **Autonomous execution** — Grok Build should be able to work on these phases with minimal on-the-fly input.
- **Verification-first** — every phase ends with full `make test` + `make test-chaos` + doctor + E2E run.

---

## 4. How to Use This Plan

Start with **Phase 1** (Core Runtime). Each phase has its own detailed task breakdown in `docs/no-stubs-plan/phase-X.md`.

When a phase is complete, mark it ✅ in this document and move to the next.

**Current Active Phase:** Phase 1

**Last Updated:** May 27, 2026
