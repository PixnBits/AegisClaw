# CLI Specification

## Overview
The AegisClaw CLI (`aegis`) is the primary power-user interface. It is lightweight, scriptable, and fully automatable.

## Connection Model
- The CLI connects **exclusively** to the Host Daemon via a **Unix domain socket** (`~/.aegis/daemon.sock` on Linux/macOS, or equivalent named pipe on Windows).
- All CLI commands are routed through the Host Daemon → AegisHub.
- No direct communication from CLI to any microVM.

## Privilege Model
- **Only** the `aegis start` (and `aegis install` during initial setup) commands require `sudo` / elevated privileges.
- All other commands (`chat`, `status`, `tasks`, `skills`, etc.) run as the regular user.
- The Host Daemon runs with the minimal host privileges needed to manage Firecracker/Docker sandboxes.

## Persistent Data Storage
- User-specific data is stored in `~/.aegis/` (created automatically on first run).
- Ownership: `chown -R $USER:$USER ~/.aegis`
- Permissions: `chmod 0700 ~/.aegis` (strict user-only access)
- Long-term persistent state (proposals, audit logs, skill registry) lives inside the Store VM; the `~/.aegis/` directory only holds local config, socket, and cache.

## Core Principles
- Everything that can be done in the Web Portal must also be possible (and often faster) via CLI
- All commands support `--json` for machine parsing and `--headless` for non-interactive use
- Consistent verbs: `list`, `show`, `status`, `new`, `grant`, `revoke`, etc.
- Strong defaults with helpful prompts and clear error messages

## Command Groups

### System & Setup
- `aegis doctor` — Verify prerequisites and system health
- `aegis status [--json]` — Overall system + security posture
- `aegis start` / `aegis stop` / `aegis restart`
- `aegis vm list [--json]`

### Conversations & Agents
- `aegis chat [--headless] [initial-prompt]`
- `aegis sessions list [--json]`
- `aegis sessions status <session-id> [--watch]`
- `aegis sessions kill <session-id>`

### Tasks & Monitoring
- `aegis tasks list [--json]`
- `aegis tasks status <task-id> [--watch]`
- `aegis tasks pause/resume/cancel <task-id>`

### Autonomy
- `aegis autonomy show <session-id>`
- `aegis autonomy grant <session-id> --preset=<name> [--duration=30m]`
- `aegis autonomy revoke <session-id> [--scope=...]`
- `aegis autonomy reset <session-id>`

### Teams (Multi-Agent)
- `aegis team new <goal> [--roles=researcher,analyst,...]`
- `aegis team list [--json]`
- `aegis team status <team-id>`
- `aegis team message <team-id> @role "message"`

### Skills & Governance
- `aegis skills propose`
- `aegis skills list [--json]`
- `aegis skills status <skill-id>`
- `aegis court decisions list [--json]`
- `aegis court decisions show <decision-id>`

### Audit & Verification
- `aegis audit log [--filter...] [--json]`
- `aegis audit verify [--all]`

## Testability Requirements
- All list/status commands must support `--json`
- Deterministic exit codes
- Support for waiting patterns (`--wait-until=ready`)

## Related Documents
- `../user-journeys/` (all 9 journeys)
- `../../prd/user-experience-principles.md`
- `../host-daemon.md`
- `../aegishub.md`