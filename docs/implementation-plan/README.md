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

## Current Priority Order (Aligned to Roadmap Phases & Gaps)

### Phase 0 Completion & CLI Gaps
- `01-cli-full-coverage.md` — Implement missing CLI verbs from specs/cli.md and gaps.md

### Paranoid Security & TCB Hardening (NEW — High Priority)
- `02-daemon-minimal-tcb-refactor.md` — Refactor Host Daemon to strictly match `docs/specs/host-daemon.md` (remove all non-TCB logic; target <2000 LOC, <20MB idle)
- `03-sandbox-lifecycle-containment.md` — Enforce daemon-only lifecycle management + crash containment + watchdog for AegisHub/Store/Network Boundary
- `04-audit-merkle-signing-hardening.md` — Isolate Merkle root signing; add static-binary verification and capability dropping
- `05-unix-socket-hardening.md` — **Avoid Docker single-socket risks**: per-client verification, strict permissions, SO_PEERCRED, non-root CLI, input validation

### Filesystem & Configuration (NEW)
- `06-directory-layout.md` — Implement single `~/.aegis/` root with paranoid permissions + move privileged socket out of home dir

### Codebase Divergences & Security Posture (Deep Analysis — NEW)
- `07-daemon-tcb-extraction.md` — Extract business logic (court, builder, dashboard, event dispatcher) out of daemon into AegisHub / dedicated components
- `08-runtime-permission-enforcement.md` — Add O_NOFOLLOW + ownership/permission checks on secrets/, data/store/, data/audit/ on every access
- `09-missing-cli-verbs.md` — Implement remaining CLI verbs: team *, autonomy grant/revoke/reset, court decisions show, full restart, skills status
- `10-semantic-skill-discovery.md` — Implement list_skills() / list_tools() with vector semantic search in Agent Runtime
- `11-workspace-customization.md` — Load AGENTS.md, SOUL.md, TOOLS.md, SKILL.md from ~/.aegis/workspace/ on agent start
- `12-eventbus-full-background.md` — Complete EventBus with timers, signals, approval queues, background service management
- `13-governance-court-full.md` — Implement full 7-persona Court + Court Scribe integration per specs/governance-court.md
- `14-builder-advanced-gates.md` — Add full SAST/SCA/policy-as-code enforcement + health checks (beyond current SBOM)
- `15-user-journey-automation.md` — Automate remaining User Journeys #2–#9 (Playwright + integration tests)

### Later Phases Alignment
- `16-phase-1-journeys.md` — Journeys #4 and #9 (Governance & SDLC heavy)
- `17-multi-agent-teams.md` — Journey #8 (Phase 3)
- `18-autonomy-controls.md` — Journey #7
- `19-final-polish.md` — Performance, resource limits, security review (Phase 4)

## Acceptance Criteria
- All 9 User Journeys have automated tests (Playwright + integration)
- 90%+ coverage on new/changed code
- **Host Daemon strictly limited to spec** (no business logic, minimal privileges, static binary, <20MB idle)
- **Unix socket is hardened** (no single-socket privilege escalation path like Docker)
- **Single predictable `~/.aegis/` root** with correct paranoid permissions + runtime enforcement on secrets
- No bypass of security gates
- Full alignment with `docs/specs/` and `docs/prd/`

**Next Step**: Start with `01-cli-full-coverage.md`, then immediately tackle `02-daemon-minimal-tcb-refactor.md` → `06-directory-layout.md` → `07-daemon-tcb-extraction.md`

*Generated from deep code analysis (May 2026). Includes divergences, stubs, and security posture issues found in cmd/aegisclaw/ and internal/.*