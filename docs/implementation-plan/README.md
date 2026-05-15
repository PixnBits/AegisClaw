# AegisClaw Implementation Plan — Delta Resolution

**Purpose**: Step-by-step tasks to bring the implementation in sync with the updated requirements, specifications, PRDs, and roadmap in `docs/`.

This plan supersedes the historical one from the `docs/lessons-learned` branch. It focuses on **closing identified gaps** (see `docs/specs/additional-requirements-and-gaps.md`) and completing automated tests for all user journeys per the **Golden Rule** in `AGENTS.md` and `docs/roadmap.md`.

**Paranoid Security Emphasis**: Every step must strictly enforce the **Minimal Trusted Computing Base (TCB)** principle from `docs/architecture.md` and `docs/specs/host-daemon.md`. The Host Daemon must remain tiny and contain **zero** business logic or untrusted data processing. **Avoid Docker-style single-socket anti-patterns** at all costs.

## How to Use This Plan

1. **Always start here** before any implementation task.
2. Take **one file at a time**.
3. Implement **exactly** what the step describes.
4. Write thorough tests (see `docs/testing-standards.md`).
5. Update `docs/CHANGELOG.md` for any new capability.
6. Run full test suite and `./aegisclaw eval run` before marking complete.
7. When a step is done, update its status or move to `docs/implementation-plan/completed/` (create if needed).

## Recommended Order (Dependencies & Risk Minimized)

### Phase 0 / Quick Wins
- `01-cli-full-coverage.md` — Implement missing CLI verbs (unblocks testing & doctor command)

### Core Infrastructure (Do These First — Everything Depends On Them)
- `02-directory-layout.md` — Single `~/.aegis/` root + move privileged socket to `/run/user/$UID/aegis/`
- `03-daemon-minimal-tcb-refactor.md` — Clean minimal daemon base (remove business logic)
- `04-unix-socket-hardening.md` — SO_PEERCRED, non-root CLI, strict permissions (now that layout exists)
- `05-runtime-permission-enforcement.md` — O_NOFOLLOW + ownership checks on secrets/, data/store/, data/audit/ on every access

### Paranoid Security Hardening
- `06-sandbox-lifecycle-containment.md` — Daemon-only lifecycle + crash containment + watchdog
- `07-audit-merkle-signing-hardening.md` — Isolated signing + static-binary verification + capability dropping

### Extraction & Cleanup
- `08-daemon-tcb-extraction.md` — Extract court, builder, dashboard, event dispatcher out of daemon (now safe)

### Feature Implementation (Parallelizable After Core Is Solid)
- `09-missing-cli-verbs.md` — Remaining verbs: team *, autonomy grant/revoke/reset, court decisions show, skills status
- `10-semantic-skill-discovery.md` — list_skills() / list_tools() with vector semantic search
- `11-workspace-customization.md` — Load AGENTS.md, SOUL.md, TOOLS.md, SKILL.md from ~/.aegis/workspace/
- `12-eventbus-full-background.md` — Timers, signals, approval queues, background services
- `13-governance-court-full.md` — Full 7-persona Court + Court Scribe
- `14-builder-advanced-gates.md` — Full SAST/SCA/policy-as-code + health checks + rollback
- `15-user-journey-automation.md` — Automate Journeys #2–#9 (Playwright + integration)

### Additional Gaps & Hardening (NEW)
- `16-resource-quotas-host-protection.md` — Prevent resource exhaustion and protect the host
- `17-threat-model-validation.md` — Implement and validate against `docs/specs/threat-model.md`
- `18-skill-dependency-management.md` — Proper dependency tracking, versioning, and secure composition

### Later Phases Alignment
- `19-phase-1-journeys.md` — Journeys #4 and #9 (Governance & SDLC heavy)
- `20-multi-agent-teams.md` — Journey #8 (Phase 3)
- `21-autonomy-controls.md` — Journey #7
- `22-final-polish.md` — Performance, resource limits, security review (Phase 4)

## Acceptance Criteria
- All 9 User Journeys have automated tests (Playwright + integration)
- 90%+ coverage on new/changed code
- **Host Daemon strictly limited to spec** (no business logic, minimal privileges, static binary, <20MB idle)
- **Unix socket is hardened** (no single-socket privilege escalation path like Docker)
- **Single predictable `~/.aegis/` root** with correct paranoid permissions + runtime enforcement on secrets
- No bypass of security gates
- Full alignment with `docs/specs/` and `docs/prd/`

**Next Step**: Start with `01-cli-full-coverage.md`, then immediately tackle `02-directory-layout.md` → `03-daemon-minimal-tcb-refactor.md` → `04-unix-socket-hardening.md` → `05-runtime-permission-enforcement.md`

*Reordered for optimal dependencies and minimal rework (May 2026). Includes additional gaps from deep analysis.*