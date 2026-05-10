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
- SBOM (CycloneDX) generation

### 5. EventBus & Background Services
- Internal event bus for scheduled tasks, timers, signals
- Background service management
- Approval queues for proactive actions

## Remaining Open Questions
- Global configuration system (Viper-style layering)
- Resource quotas and host protection
- TUI (Bubble Tea)
- Full threat model
- Skill dependency management
- Backup / restore strategy

## Confirmed Remaining Gaps In This Branch

- **CLI coverage (`docs/specs/cli.md`)**: `restart`, `team *`, `skills status`, `court decisions show`, session/task status and control verbs, autonomy grant/revoke/reset, `audit verify`, and the CLI secrets lifecycle are not implemented end-to-end yet.
- **Journey automation (`docs/tasks/phase-0-foundations.md`, `docs/roadmap.md`)**: only User Journey #1 is currently automated in CI; journeys #2-#9 are still partial, placeholder, or documentation-only.
- **Host Daemon (`docs/specs/host-daemon.md`)**: watchdog behavior, audit-root signing, static-binary verification, socket-hardening tests, and lifecycle-containment coverage remain incomplete.
- **AegisHub (`docs/specs/aegishub.md`)**: ACL hot reload, denied-message audit persistence, and fuller handshake/signature enforcement coverage still need implementation.
- **Web Portal (`docs/specs/web-portal.md`)**: dedicated skills/proposals/court/autonomy flows and the stable selectors needed to automate those later journeys are not implemented yet.
- **Operational scripts referenced by CI**: the repository does not yet contain the image-build and live-test scripts that future phases expect under `scripts/`.

## Next Actions
- Create dedicated specs for the top 5 items above
- Update relevant PRD and architecture docs
