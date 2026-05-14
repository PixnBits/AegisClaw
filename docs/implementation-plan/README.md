# AegisClaw Implementation Plan — Delta Resolution

**Purpose**: Step-by-step tasks to bring the implementation in sync with the updated requirements, specifications, PRDs, and roadmap in `docs/`.

This plan supersedes the historical one from the `docs/lessons-learned` branch. It focuses on **closing identified gaps** (see `docs/specs/additional-requirements-and-gaps.md`) and completing automated tests for all user journeys per the **Golden Rule** in `AGENTS.md` and `docs/roadmap.md`.

**Paranoid Security Emphasis**: Every step must strictly enforce the **Minimal Trusted Computing Base (TCB)** principle from `docs/architecture.md` and `docs/specs/host-daemon.md`. The Host Daemon must remain tiny and contain **zero** business logic or untrusted data processing.

## How to Use This Plan

1. **Always start here** before any implementation task.
2. Take **one file at a time**.
3. Implement **exactly** what the step describes.
4. Write thorough tests (see `docs/testing-standards.md`).
5. Update `docs/CHANGELOG.md` for any new capability.
6. Run full test suite and `./aegisclaw eval run` before marking complete.
7. When a step is done, update its status or move to `docs/implementation-plan/completed/` (create if needed).

## Current Priority Order (Aligned to Roadmap Phases & Gaps)

### Phase 0 Completion & CLI Gaps
- `01-cli-full-coverage.md` — Implement missing CLI verbs from specs/cli.md and gaps.md

### Paranoid Security & TCB Hardening (NEW — High Priority)
- `02-daemon-minimal-tcb-refactor.md` — Refactor Host Daemon to strictly match `docs/specs/host-daemon.md` (remove all non-TCB logic; target <2000 LOC, <20MB idle)
- `03-sandbox-lifecycle-containment.md` — Enforce daemon-only lifecycle management + crash containment + watchdog for AegisHub/Store/Network Boundary
- `04-audit-merkle-signing-hardening.md` — Isolate Merkle root signing; add static-binary verification and capability dropping

### Additional Requirements (from specs/additional-requirements-and-gaps.md)
- `05-skill-tool-discovery.md` — Implement `list_skills()`, `list_tools()`, semantic search in Agent Runtime
- `06-workspace-customization.md` — Load AGENTS.md, SOUL.md, TOOLS.md, SKILL.md from `~/.aegis/workspace/`
- `07-secrets-vault.md` — Full CLI secrets lifecycle + encrypted storage + Network Boundary injection
- `08-advanced-builder-gates.md` — SAST/SCA/secrets scanning, policy-as-code, SBOM (already partial), health checks
- `09-eventbus-background.md` — Internal EventBus, timers, signals, approval queues

### Core Component Hardening (Gaps)
- `10-host-daemon-hardening.md` — (Now covered by 02-04; keep for socket hardening tests)
- `11-aegishub-acl-reload.md` — Hot reload, denied-message audit, fuller handshake
- `12-web-portal-completion.md` — Skills/proposals/court/autonomy flows + stable selectors for E2E
- `13-operational-scripts.md` — image-build and live-test scripts under `scripts/`

### Later Phases Alignment
- `14-phase-1-journeys.md` — Journeys #4 and #9 (Governance & SDLC heavy)
- `15-multi-agent-teams.md` — Journey #8 (Phase 3)
- `16-autonomy-controls.md` — Journey #7
- `17-final-polish.md` — Performance, resource limits, security review (Phase 4)

## Acceptance Criteria
- All 9 User Journeys have automated tests (Playwright + integration)
- 90%+ coverage on new/changed code
- **Host Daemon strictly limited to spec** (no business logic, minimal privileges, static binary, <20MB idle)
- No bypass of security gates
- Full alignment with `docs/specs/` and `docs/prd/`

**Next Step**: Start with `01-cli-full-coverage.md`, then immediately tackle `02-daemon-minimal-tcb-refactor.md`

*Generated to resolve delta between implementation and updated docs (May 2026). Paranoid security TCB steps added per user request.*