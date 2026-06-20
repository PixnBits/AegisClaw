# SDLC Web Portal Specification

**Status:** Draft  
**Last Updated:** June 2026  
**Origin:** Migrated from [GitHub Issue #35](https://github.com/PixnBits/AegisClaw/issues/35) (`docs/issue-35.md`, now deprecated)

## Documentation Layout (June 2026)

Recent commits reorganized portal documentation. When cross-referencing:

- **Target-state portal specs** live under `docs/specs/web-portal/` (added in #74). Start with `web-portal.md` for IA, principles, and page behaviors; per-page specs cover channels, court, dashboard, canvas, real-time contracts, testing, etc.
- **Implementation-current snapshot** remains at `docs/specs/web-portal.md` (legacy monolith spec; tracks what ships today).
- **Removed:** `docs/specs/web-portal-screens.md` (legacy wireframes — superseded by modular specs in `docs/specs/web-portal/`).
- **PRD:** `docs/PRD/web-portal/index.md` captures the redesign direction.
- **This document** is the canonical SDLC-specific vision (proposal → Court → build → PR → deploy transparency). It complements, not replaces, the general portal specs.

## Purpose

Provide comprehensive visibility and control over the complete Software Development Life Cycle (SDLC) for skill addition through a rich web portal. This enables transparent, auditable development while maintaining paranoid security.

## Vision

Transform skill development into a GitHub-like internal development experience with full Court involvement at key stages (Proposal Review, Code Review, Deployment Approval), while preserving the paranoid-by-design architecture.

## Expanded SDLC Phases with Portal Features

### Phase 1: Ideation & Requirements
- Proposal editor with schema validation
- Threaded discussion between User and Court

### Phase 2: Design Review (Pre-Implementation)
- Court dashboard with live review status
- Risk scoring and evidence-based verdicts

### Phase 3: Implementation (Code Generation)
- Live build log streaming
- Source code browser for generated files
- Git diff viewer (main vs proposal branch)

### Phase 4: Code Review (Post-Implementation)
- PR-style interface with inline comments
- Court personas request changes
- Iteration tracking

### Phase 5: Security Gates & Build
- Security scan results with file/line links
- SBOM viewer with CVE cross-reference
- Vulnerability details and remediation guidance

### Phase 6: Pre-Deployment Review
- Deployment preview with resource estimates
- Risk re-assessment and impact analysis
- One-click approve/reject with signature

### Phase 7: Deployment & Monitoring
- Deployment status and health dashboard
- Skill invocation logs and performance metrics
- One-click rollback

## Key Features

### 1. Source Code Browser
- File tree navigation for `skills/` and `self/` repositories
- Syntax highlighting (Go, Python, JS, YAML, Markdown)
- Workspace editor for `SOUL.md`, `AGENTS.md`, `TOOLS.md`, `<skill>.SKILL.md`
- Read-only mode for generated code by default

### 2. Git History & Diff Viewer
- Commit timeline with Merkle audit links
- Branch viewer and comparison
- Side-by-side or unified diff view
- Commit detail pages with changed files summary

### 3. Internal Pull Request System
- Auto-created PR after builder completes
- PR overview with diff summary + security results
- Inline code comments (threaded, resolvable)
- Court code review integration with weighted consensus
- Iteration support (request changes → builder re-runs)

### 4. Live Build Dashboard
- Real-time progress via STOMP topic subscriptions (transitioning from SSE)
- Build steps breakdown with status/duration/logs
- Security gate results (expandable)
- Sandbox resource monitoring
- SBOM viewer

### 5. Discussion Threads
- Proposal-level and PR-level threaded discussions
- Court persona responses (via isolated microVMs)
- Notification system (dashboard + optional native)

### 6. Deployment Approval Gate
- Deployment preview (resources, network, secrets, manifest diff)
- Risk re-assessment UI
- Approve/Reject with cryptographic signature
- Timeout + emergency stop

## Architectural Principles

- **Portal Isolation**: Web Portal runs in a dedicated Firecracker microVM (`docs/specs/web-portal-vm.md`); presentation-only, all actions mediated via vsock bridge
- **Read-Only by Default**: All git/workspace access read-only from UI
- **Full Auditability**: Every UI action (view, comment, approve) logged to Merkle tree
- **No Bypass**: All security gates remain mandatory
- **Performance**: Page loads < 300ms; real-time updates via targeted STOMP topics (see `docs/specs/web-portal/real-time-contracts.md`)

## Technology Stack

**Backend (Go):**
- `internal/dashboard/server.go` (portal HTTP + bridge)
- PR handlers in `internal/dashboard/dashboard_pr_handlers.go`
- Reuse: `internal/git/`, `internal/builder/`, `internal/court/`

**Frontend:**
- Self-contained `html/template` + embedded CSS/JS (no CDNs)
- Monaco Editor or CodeMirror for code viewing (planned)
- diff2html or custom rendering
- STOMP-over-WebSocket for live updates; SSE retained for transition compatibility

## Implementation Phases (from Issue #35)

- **Phase 2**: Source Code Viewer + Workspace Editor
- **Phase 3**: Git History & Diff Viewer
- **Phase 4**: Pull Request System + Court Code Review
- **Phase 5**: Live Build Dashboard
- **Phase 6**: Discussion Threads + Deployment Approval Gate
- **Phase 7**: Polish, Accessibility, Security Hardening

## Success Metrics
- 100% of SDLC phases visible in web portal
- Court code review adoption > 80% for medium+ risk skills
- Zero bypass of deployment approval gate
- Page loads < 300ms; real-time latency < 1s

## Open Questions
- Should Court code review be mandatory for all skills or only medium+ risk?
- How many builder iterations allowed before escalation?
- Deployment approval timeout policy?
- Multi-user support timeline?

## Related Documents

**SDLC & governance:**
- `docs/prd/sdlc-governance.md`
- `docs/specs/governance-court.md`
- `docs/specs/builder-security-gates.md`
- `docs/architecture.md`

**Portal (target state — `docs/specs/web-portal/`):**
- `web-portal.md` — overall IA, principles, navigation
- `dashboard.md` — monitoring and intervention
- `court.md` — governance flows
- `canvas.md` — pipeline visualization
- `real-time-contracts.md` — STOMP topics for build/PR/proposal updates
- `implementation-gaps-and-priorities.md` — remaining open areas

**Portal (implementation current):**
- `docs/specs/web-portal.md` — shipped features, API surface, styling
- `docs/specs/web-portal-vm.md` — isolation model
- `docs/specs/chat-ui-data-flow.md` — chat streaming

**Testing & traceability:**
- `docs/testing-standards.md`
- `web_portal_e2e_sdlc_test.go`
- `cmd/aegisclaw/portal_contract_test.go`

**Historical:**
- `docs/issue-35.md` — original issue text (deprecated; retained for audit trail)
- [GitHub Issue #35](https://github.com/PixnBits/AegisClaw/issues/35)