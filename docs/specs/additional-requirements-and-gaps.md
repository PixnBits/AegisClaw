# Additional Requirements & Identified Gaps from v1 Codebase

## Incorporated Items

### 1. Skill / Tool Discovery & Lookup
Agents must be able to dynamically query available skills and tools at runtime.

- Dedicated tool: `list_skills()`, `list_tools()`, or `get_capabilities()`
- Returns: name, description, required scopes, version, status
- Should support semantic search (vector embeddings)
- Must be fast and available in every Agent Runtime VM

### 2. Workspace Customization
Support loading user-defined context files from `~/.aegis/workspace/`:
- `AGENTS.md` — custom agent personas
- `SOUL.md` — system soul / values
- `TOOLS.md` — tool descriptions
- `SKILL.md` — skill templates

This enables strong personalization.

### 3. Vault / Secrets Management
- CLI-only: `aegis secrets set/list/remove`
- Interactive prompt or `--stdin` / `--file`
- Encrypted storage (age + HKDF recommended)
- Safe injection via Network Boundary only
- Memory zeroing after use

### 4. Advanced Skill Lifecycle (Builder)
- SAST, SCA, secrets scanning
- Policy-as-code enforcement
- Composition with health checks + automatic rollback
- SBOM (CycloneDX) generation — implemented in 7.8 via `make sbom` + Builder VM hooks + cosign placeholders (see Makefile, scripts/build-microvms-docker.sh, threat-model.md:3). Fallback manifest always produced; full JSON when cyclonedx-gomod/syft present.

### 5. EventBus & Background Services
- Internal event bus for scheduled tasks, timers, signals
- Background service management
- Approval queues for proactive actions

### 6. SDLC & Code Development Workflow Documentation (June 2026)
Consolidated, implementer-focused documentation to enable safe self-improvement and skill creation:

- New `docs/specs/sdlc-code-development-workflow.md` — single entry point with core flow, immutable contracts, key invariants, and "For Implementers" guidance.
- "For Implementers" sections added to `builder-vm.md` and `store-vm.md`.
- Machine-readable command surface in `sdlc-commands.yaml` (verbs, owners, invariants, permissions notes).
- Cross-references and permissions integration notes added across the SDLC specs.
- This directly supports automated agents (Grok Build /goal mode or equivalent) while remaining future-proof for human maintainers.

## Remaining Open Questions
- Global configuration system (Viper-style layering)
- Resource quotas and host protection
- TUI (Bubble Tea)
- Full threat model
- Skill dependency management
- Backup / restore strategy

## Confirmed Remaining Gaps In This Branch

- **CLI coverage (`docs/specs/cli.md`)**: `restart`, `team *`, `skills status`, `court decisions show`, session/task status and control verbs, autonomy grant/revoke/reset, `audit verify`, and the CLI secrets lifecycle are not implemented end-to-end yet.
- **Journey automation (`docs/tasks/phase-0-foundations.md`, `docs/roadmap.md`)**: Major progress in Phase 5 Group 3. All 9 journeys now have dedicated or strongly enhanced automated Playwright E2E coverage in `e2e/journeys.spec.js`, including explicit failure + recovery paths, using stable data-testid from Groups 1-2. Some journeys remain fixture-heavy; full live daemon coverage is expected in later phases. (See `docs/no-stubs-plan/phase-5.md` Group 3 completion notes.)
- **Host Daemon (`docs/specs/host-daemon.md`)**: watchdog behavior, audit-root signing, static-binary verification, socket-hardening tests, and lifecycle-containment coverage remain incomplete.
- **AegisHub (`docs/specs/aegishub.md`)**: ACL hot reload, denied-message audit persistence, and fuller handshake/signature enforcement coverage still need implementation.
- **Web Portal (`docs/specs/web-portal/implementation-current.md`)**: Significant progress across Phase 5 Groups 1-3. 
  - Handlers for Git, Workspace, Memory, Approvals, Canvas, and full streaming Markdown chat are wired with deterministic E2E fixture support.
  - Stable `data-testid` added across all major surfaces (G1/G2).
  - All 9 user journeys have automated E2E coverage (G3), including failure + recovery.
  - Public REST surface (`/api/proposals*`, `/api/approvals`, etc.) is substantially implemented via thin delegation.
  - Residuals remain: some `git.*`/`workspace.*`/`dashboard.skills` actions may still be partial in the live daemon path (delegation to Store/Builder); certain advanced `/api/*` endpoints (rich proposal status, build logs) are still thin or fixture-only. See `docs/no-stubs-plan/phase-5.md` Group 4 working list for specific next targets.
- **Operational scripts referenced by CI**: the repository does not yet contain the image-build and live-test scripts that future phases expect under `scripts/`.

## Next Actions
- Create dedicated specs for the top 5 items above
- Continue tightening CLI, web-portal delegation to Store/Builder, and live (non-fixture) paths
- Maintain `sdlc-commands.yaml` and the workflow overview as living contracts when new verbs or enforcement points are added
