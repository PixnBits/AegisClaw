# SDLC Web Portal Specification

**Status:** Draft  
**Last Updated:** May 2026
**Related:** Issue #35, `docs/specs/web-portal-screens.md`, `docs/web-dashboard.md`

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
- Real-time progress with SSE
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

- **Dashboard Isolation**: Dashboard runs in daemon (root) initially; consider dedicated microVM in Phase 6
- **Read-Only by Default**: All git/workspace access read-only from UI
- **Full Auditability**: Every UI action (view, comment, approve) logged to Merkle tree
- **No Bypass**: All security gates remain mandatory
- **Performance**: Page loads < 300ms, SSE latency < 1s

## Technology Stack

**Backend (Go):**
- Extend `internal/dashboard/server.go`
- New packages: `internal/pullrequest/`, `internal/discussion/`
- Reuse: `internal/git/`, `internal/builder/`, `internal/court/`

**Frontend:**
- HTMX + Tailwind CSS (existing stack)
- Monaco Editor or CodeMirror for code viewing
- diff2html or custom rendering
- Server-Sent Events for live updates

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
- Page loads < 300ms, SSE latency < 1s

## Open Questions
- Should Court code review be mandatory for all skills or only medium+ risk?
- How many builder iterations allowed before escalation?
- Deployment approval timeout policy?
- Multi-user support timeline?

## Related Documents
- `docs/specs/web-portal-screens.md`
- `docs/specs/web-portal-vm.md`
- `docs/architecture.md`
- `docs/prd/sdlc-governance.md`
- Original Issue #35 (historical context)