https://github.com/PixnBits/AegisClaw/issues/35

Enhance the AegisClaw web portal to provide comprehensive visibility and control over the complete Software Development Life Cycle (SDLC) for skill addition, from proposal through deployment. This addresses the need for a continuous, transparent development process with appropriate Governance Court and user involvement at each stage, particularly around source code management, build pipelines, reviews, and deployment workflows.

## Background

AegisClaw implements a paranoid-by-design SDLC for adding skills:
1. **Proposal Creation** → User describes desired skill
2. **Governance Court Review** → 5 AI personas evaluate proposal
3. **Builder Pipeline** → Code generation in sandboxed Firecracker VM
4. **Security Gates** → SAST, SCA, secrets scanning, policy-as-code (mandatory, no bypass)
5. **Deployment** → Versioned composition with automatic rollback

While this architecture is sound, **visibility into the continuous development process** is limited. Users and Court reviewers lack comprehensive tooling to:
- Browse and edit source code across workspaces
- Visualize git history, branches, and proposal evolution
- Review generated code as proper \"Pull Requests\"
- Monitor build pipeline status in real-time
- Conduct threaded discussions on code changes
- Approve deployments with full context

## Current State Analysis

### What Exists Today

#### 1. **Git Infrastructure** (`internal/git/manager.go`)
- ✅ Dual repository structure: `skills/` (user skills) and `self/` (kernel)
- ✅ Proposal branches (`proposal-<id>`) created from main
- ✅ All commits signed by kernel with Ed25519
- ✅ Merkle audit log for all git operations
- ⚠️ **Limited**: No web-based visualization or PR workflow

#### 2. **Builder Pipeline** (`internal/builder/pipeline.go`)
- ✅ Complete build orchestration with security gates
- ✅ SBOM generation (CycloneDX 1.4)
- ✅ File hashes and diff generation
- ✅ Artifact signing
- ⚠️ **Limited**: Build status visible only through CLI/status commands, no live dashboard

#### 3. **Web Dashboard** (`internal/dashboard/server.go`, `docs/web-dashboard.md`)
**Implemented:**
- Overview page (system health, recent activity)
- Skills & Proposals listing
- Memory Vault with semantic search
- Audit Log Explorer
- Settings page with PII controls

**Planned but Not Implemented:**
- Source code browser
- Git history visualization
- Pull Request interface
- Build pipeline live view
- Threaded code review discussions
- Deployment approval workflow

#### 4. **Governance Court** (`internal/court/`)
- ✅ 5 personas review proposals in isolated microVMs
- ✅ Weighted consensus with risk scoring
- ✅ Structured verdicts with evidence
- ⚠️ **Limited**: Reviews happen on proposal text/schema, not on actual generated code diffs

#### 5. **Workspace Management** (`internal/workspace/workspace.go`)
- ✅ User workspace at `~/.aegisclaw/workspace/`
- ✅ Custom files: `SOUL.md`, `AGENTS.md`, `TOOLS.md`, `<skill>.SKILL.md`
- ⚠️ **Limited**: No web interface for editing or viewing workspace files

### Current SDLC Flow

```
User Request (CLI/Chat)
    ↓
Proposal Creation (proposal.create_draft)
    ↓
Court Review (5 personas in microVMs)
    ↓ [if approved]
Builder Pipeline Launch (sandboxed Firecracker VM)
    ↓
Code Generation (LLM generates skill code)
    ↓
Git Commit to proposal-<id> branch
    ↓
Security Gates (SAST, SCA, secrets, policy) ← MANDATORY, NO BYPASS
    ↓ [if passed]
Artifact Signing + SBOM
    ↓
Deployment to Composition Manifest
    ↓
Skill Activation (new Firecracker microVM)
```

**Key Gaps:**
1. No source code visibility during/after generation
2. Court reviews proposal *before* seeing actual code
3. No PR-style review of generated code
4. Build pipeline is \"black box\" until completion
5. No workspace file management in UI
6. No deployment review gate with full context

## Ideal State: Continuous SDLC with Court Involvement

### Vision: GitHub-like Internal Development Experience

The ideal state treats skill development as a **complete software project lifecycle** with transparency at every stage, while maintaining paranoid security.

### Expanded SDLC Flow with Personas

```
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 1: Ideation & Requirements                                    │
│ Personas: User + UserAdvocate + Main Agent                          │
├─────────────────────────────────────────────────────────────────────┤
│ • User describes need via chat/CLI                                  │
│ • Main agent refines requirements (tools, network, secrets)         │
│ • UserAdvocate provides early UX feedback                           │
│ • Draft proposal created                                            │
│ → WEB PORTAL: Proposal editor with schema validation               │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 2: Design Review (Pre-Implementation)                         │
│ Personas: Full Court (CISO, Coder, SecurityArchitect, Tester, UA)  │
├─────────────────────────────────────────────────────────────────────┤
│ • Weighted consensus on proposal text                               │
│ • Risk scoring, security posture evaluation                         │
│ • Evidence-based verdicts                                           │
│ → WEB PORTAL: Court dashboard with live review status              │
│ → Enhancement: Threaded Q&A between user and Court                  │
└─────────────────────────────────────────────────────────────────────┘
                              ↓ [approved]
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 3: Implementation (Code Generation)                           │
│ Personas: Builder VM (LLM) + Coder persona observing               │
├─────────────────────────────────────────────────────────────────────┤
│ • Builder VM spawned in Firecracker sandbox                         │
│ • Code generation with iteration support                            │
│ • Commits to proposal-<id> branch                                   │
│ → WEB PORTAL: Live build log streaming (ephemeral sandbox view)    │
│ → WEB PORTAL: Source code browser for generated files              │
│ → WEB PORTAL: Git diff viewer (main vs proposal branch)            │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 4: Code Review (Post-Implementation)                          │
│ Personas: Coder, SecurityArchitect, Tester, CISO                   │
├─────────────────────────────────────────────────────────────────────┤
│ • **NEW**: Court reviews actual generated code                      │
│ • Line-by-line security analysis                                    │
│ • Architecture pattern validation                                   │
│ • Test coverage assessment                                          │
│ → WEB PORTAL: PR-style interface with inline comments              │
│ → WEB PORTAL: Court personas can request changes                   │
│ → WEB PORTAL: Change tracking across iterations                    │
└─────────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 5: Security Gates & Build                                     │
│ Personas: Automated (SAST, SCA, secrets, policy)                   │
├─────────────────────────────────────────────────────────────────────┤
│ • MANDATORY gates (no bypass)                                       │
│ • Finding severity classification                                   │
│ • Blocking vs. advisory findings                                    │
│ → WEB PORTAL: Security scan results with file/line links           │
│ → WEB PORTAL: Vulnerability details and remediation guidance       │
│ → WEB PORTAL: SBOM viewer (dependencies with CVE cross-reference)  │
└─────────────────────────────────────────────────────────────────────┘
                              ↓ [gates passed]
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 6: Pre-Deployment Review                                      │
│ Personas: CISO, User, SecurityArchitect                            │
├─────────────────────────────────────────────────────────────────────┤
│ • Review final artifact with SBOM                                   │
│ • Deployment impact analysis (composition manifest diff)            │
│ • Network/secrets policy confirmation                               │
│ • Rollback plan verification                                        │
│ → WEB PORTAL: Deployment preview with resource estimates           │
│ → WEB PORTAL: Approval widget (approve/reject with signature)      │
│ → WEB PORTAL: Composition version timeline                         │
└─────────────────────────────────────────────────────────────────────┘
                              ↓ [approved]
┌─────────────────────────────────────────────────────────────────────┐
│ Phase 7: Deployment & Monitoring                                    │
│ Personas: User, System                                              │
├─────────────────────────────────────────────────────────────────────┤
│ • Skill activated in new Firecracker microVM                        │
│ • Health checks and automatic rollback on failure                   │
│ • Runtime monitoring                                                │
│ → WEB PORTAL: Deployment status and health dashboard               │
│ → WEB PORTAL: Skill invocation logs and performance metrics        │
│ → WEB PORTAL: One-click rollback to previous version               │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Principles of Ideal State

1. **Transparency**: Every phase visible in web portal
2. **Court Involvement**: Proposal review (phase 2) + Code review (phase 4) + Deployment review (phase 6)
3. **Continuous Feedback**: Threaded discussions, iteration support
4. **Security First**: All gates enforced, no shortcuts
5. **User Control**: Approve/reject at critical gates
6. **Auditability**: Full Merkle log trail with rich context

## Specific Gaps and Recommendations

### Gap 1: Source Code Visibility and Workspace Management

**Current State:**
- Generated code exists in git but no web UI to browse it
- Workspace files (`SOUL.md`, `AGENTS.md`, etc.) editable only via filesystem
- No syntax highlighting or code navigation

**Recommendation: Source Code Tab**

Implement a comprehensive source code browser with:

#### Features:
1. **File Tree Navigation**
   - Browse `skills/` and `self/` repositories
   - Expand/collapse directory structure
   - File type icons and syntax highlighting
   - Search across files (integrated with existing grep/glob)

2. **Code Viewer**
   - Syntax highlighting (Go, Python, JavaScript, YAML, Markdown)
   - Line numbers
   - Permalink to specific lines
   - Raw view / download file options
   - Copy to clipboard

3. **Workspace Editor**
   - Edit `SOUL.md`, `AGENTS.md`, `TOOLS.md` directly in browser
   - Per-skill `<skill>.SKILL.md` management
   - Auto-save with git commit (audit logged)
   - Preview for Markdown files

4. **Security Considerations**
   - Read-only mode by default for generated skill code
   - Workspace edits require authentication (future: mTLS token)
   - All edits logged to Merkle audit log with file diffs
   - No ability to directly edit proposal branches (only view)

**Implementation Path:**
- Backend: Extend `internal/dashboard/server.go` with file browsing endpoints
- Leverage existing `internal/git/manager.go` for repository access
- Frontend: HTMX + Monaco Editor (minimal JS) or CodeMirror
- Phase: Add to Phase 4 of implementation plan

### Gap 2: VCS History and Branch Visualization

**Current State:**
- Git operations happen but no web visualization
- Proposal branches created automatically
- No diff viewing or commit history exploration

**Recommendation: Git History Tab**

#### Features:
1. **Commit Timeline**
   - Reverse chronological commit list
   - Commit hash (short), message, author, timestamp
   - Filter by branch, skill, date range
   - Merkle audit log link for each commit

2. **Branch Viewer**
   - List all branches (main + active proposal branches)
   - Branch comparison (ahead/behind main)
   - Visual branch graph (ASCII art or mermaid.js for simplicity)
   - Active proposal indicator

3. **Diff Viewer**
   - Side-by-side or unified diff view
   - Syntax-highlighted diffs
   - Collapse/expand hunks
   - Link to specific files from diff

4. **Commit Details Page**
   - Full diff for commit
   - Changed files summary
   - Related audit log entries
   - SBOM diff (if applicable)

**Security Considerations:**
- All git access read-only through web UI
- Commits only via builder pipeline (maintains signing)
- Branch deletion/force-push disabled in UI
- Audit log linkage for traceability

**Implementation Path:**
- Backend: Add VCS endpoints using `go-git` (already dependency)
- Reuse existing diff generation from `internal/builder/pipeline.go`
- Frontend: HTMX with diff2html or custom rendering
- Phase: Add to Phase 4

### Gap 3: Pull Request Workflow for Code Review

**Current State:**
- Court reviews proposals *before* code exists
- No second review of generated code
- Proposal branches exist but no PR concept

**Recommendation: Internal Pull Request System**

#### Features:
1. **PR Creation (Auto)**
   - Automatically create \"PR\" after builder completes
   - Links proposal → branch → code changes
   - Inherits context from original proposal

2. **PR Overview Page**
   - Title, description (from proposal)
   - Diff summary (files changed, lines added/removed)
   - Security gate results (inline)
   - SBOM attached
   - Build logs linked
   - Approval status

3. **Inline Code Comments**
   - Court personas can comment on specific lines
   - Threaded discussions
   - Resolve/unresolve states
   - User can respond to Court feedback

4. **Iteration Support**
   - Request changes → builder re-runs → new commit
   - Track iteration count (prevent infinite loops)
   - Compare iterations

5. **Approval Workflow**
   - Court members vote: Approve / Request Changes / Comment
   - Weighted consensus (same as proposal phase)
   - User can override low-risk Court rejections (logged)
   - CISO/SecurityArchitect veto power for critical findings

6. **Merge/Deploy**
   - \"Merge PR\" = deploy skill
   - Composition manifest updated
   - PR marked as merged with timestamp
   - Link to deployed skill in UI

**Security Considerations:**
- All comments logged in audit trail
- Court personas run in separate microVMs (existing architecture)
- No direct code editing in PR (only via builder iteration)
- Cryptographic signatures on approvals
- Full comment history preserved (append-only)

**Integration with Existing Court:**
- **Phase 2 (Proposal Review)**: Design-level review (what to build)
- **NEW Phase 4 (PR/Code Review)**: Implementation-level review (how it was built)
- Both phases required for high-risk skills
- Low-risk skills: PR review optional (configurable)

**Implementation Path:**
- Backend: New `internal/pullrequest/` package
- Store PR metadata in SQLite (similar to proposals)
- Reuse Court engine for code-level review
- Frontend: HTMX PR template (inspired by GitHub UI)
- Phase: Core feature for Phase 4, full polish in Phase 5

### Gap 4: Build Pipeline Transparency

**Current State:**
- Builder runs in Firecracker sandbox
- Build logs exist but only CLI accessible
- No live status updates

**Recommendation: Live Build Dashboard**

#### Features:
1. **Build Status Widget**
   - Current phase indicator (Step 1-10 of pipeline)
   - Progress bar
   - Live log streaming (SSE)
   - Elapsed time / estimated remaining

2. **Build Steps Breakdown**
   - Step 1: Launch builder sandbox
   - Step 2: Generate code (with iteration tracking)
   - Step 3-7: (existing pipeline steps)
   - Step 8.5: Security gates (expandable)
   - Step 9: Git commit
   - Step 9.5: SBOM generation
   - Step 10: Artifact signing
   - Each step: status (pending/running/success/failed), duration, logs

3. **Security Gate Results**
   - Gate-by-gate view (SAST, SCA, secrets, policy)
   - Finding count by severity
   - Click to expand findings
   - Link to file/line in source viewer

4. **Sandbox Resource Monitoring**
   - VM CPU/memory usage
   - Network activity (should be none for builder)
   - Disk usage
   - Live process tree (for transparency)

5. **Build Artifacts**
   - Generated files (with diffs)
   - SBOM download/viewer
   - Signed artifact hash
   - Audit log entries

**Security Considerations:**
- Log streaming from sandboxed VM (vsock only)
- Resource monitoring non-invasive (read-only)
- No ability to inject commands into running builder
- All artifacts signed and verified before display

**Implementation Path:**
- Backend: SSE endpoint in dashboard server
- Extend `internal/builder/pipeline.go` with progress callbacks
- Frontend: HTMX with SSE for live updates
- Phase: Add to Phase 2 (aligns with Event Bus/async work)

### Gap 5: Enhanced Review and Discussion

**Current State:**
- Court reviews structured verdicts
- No threaded discussions
- User cannot ask clarifying questions to Court

**Recommendation: Discussion Threads**

#### Features:
1. **Proposal Discussion**
   - User posts questions during refinement
   - Court personas respond (via LLM with persona prompts)
   - Main agent synthesizes answers
   - All discussion logged

2. **PR Discussion**
   - General comments (not line-specific)
   - Technical debt notes
   - Future improvement suggestions
   - Links to related proposals/skills

3. **Notification System**
   - Dashboard toast for new Court comments
   - Optional: Native OS notifications
   - Email bridge (if email skill deployed)

4. **Historical Context**
   - Link related proposals (dependencies, similar patterns)
   - \"Court has reviewed 3 similar proposals\" indicator
   - Learning from past decisions

**Security Considerations:**
- All LLM calls for Court responses in isolated microVMs
- Discussion content audited
- Rate limiting to prevent DoS via comment spam

**Implementation Path:**
- Backend: Extend proposal/PR stores with discussion threads
- Integrate with Court engine for persona responses
- Frontend: HTMX comment widgets
- Phase: Phase 4

### Gap 6: Deployment Review Workflow

**Current State:**
- Skill activates automatically after security gates pass
- No deployment-specific review gate
- Composition manifest updated silently

**Recommendation: Pre-Deployment Approval Gate**

#### Features:
1. **Deployment Preview**
   - Resource requirements (CPU, memory for new microVM)
   - Network egress destinations (if any)
   - Secrets to be injected
   - Composition manifest diff (before/after)
   - Rollback plan display

2. **Risk Re-Assessment**
   - Court provides deployment-specific risk score
   - Impact analysis: \"This skill will have network access to X\"
   - Dependency analysis: \"Uses library Y (CVE check)\"

3. **Approval UI**
   - User clicks \"Approve Deployment\" or \"Reject\"
   - Optional: Schedule deployment (timer-based)
   - Reason required for rejection
   - Signature captured (Merkle log)

4. **Post-Deployment**
   - Health check results
   - First invocation status
   - Rollback button (one-click)
   - Monitoring dashboard link

**Security Considerations:**
- Approval requires authenticated session
- Cryptographic signature on approval decision
- Timeout: Auto-reject if not approved within N hours
- Emergency stop: User can halt deployment mid-process

**Implementation Path:**
- Backend: Approval gate in composition flow
- Extend `internal/composition/manifest.go`
- Frontend: Approval modal with risk summary
- Phase: Phase 5 (after PR workflow stabilizes)

## Architectural Considerations

### Security (Paranoid Design Maintained)

1. **Web Dashboard Isolation**
   - Dashboard server runs in daemon (root process)
   - Consider: Move to dedicated microVM (Phase 6 hardening)
   - All API endpoints ACL-protected
   - CSP enforced (no external scripts)
   - mTLS optional for advanced users

2. **Data Flow**
   - All reads from audited sources (git, proposal store, audit log)
   - No direct microVM access from web UI
   - Court interactions via AegisHub (existing architecture)

3. **Authentication**
   - Initially: localhost-only, no auth required
   - Phase 6: Token-based or mTLS
   - Future: Multi-user support with RBAC

4. **Audit Logging**
   - Every UI action logged (view, edit, approve, comment)
   - Merkle tree integration
   - Tamper-evident history

### User Experience

1. **Performance**
   - Page loads < 300ms (existing goal)
   - SSE for live updates (not polling)
   - Lazy loading for large diffs/logs
   - Syntax highlighting on-demand

2. **Accessibility**
   - Keyboard navigation for code viewer
   - Screen reader support for status updates
   - High contrast mode option

3. **Responsive Design**
   - Desktop-first (primary use case)
   - Tablet-friendly (secondary)
   - Mobile: read-only view (future)

4. **Offline Resilience**
   - Dashboard should work if Ollama is down
   - Read-only mode if builder unavailable
   - Graceful degradation

### Technology Stack

**Backend (Go):**
- Extend `internal/dashboard/server.go`
- New packages: `internal/pullrequest/`, `internal/discussion/`
- Reuse: `internal/git/`, `internal/builder/`, `internal/court/`

**Frontend:**
- HTMX + Tailwind CSS (existing stack)
- Monaco Editor or CodeMirror for code viewing
- diff2html for diff rendering
- mermaid.js for git graph (optional)
- Server-Sent Events for live updates

**Data Storage:**
- SQLite for PR/discussion metadata
- Git repositories for source code
- Existing proposal/audit stores

## Implementation Roadmap

### Phase 1: Foundation (Weeks 1-2)
- ✅ Already complete per implementation-plan.md
- Snapshot management, ToolRegistry, structured output

### Phase 2: Source Code Viewer (Weeks 3-4)
- File tree browser
- Code viewer with syntax highlighting
- Workspace editor (SOUL.md, AGENTS.md)
- Basic git commit timeline

### Phase 3: Git History & Diff Viewer (Weeks 5-6)
- Branch viewer
- Full git history with filtering
- Diff viewer (side-by-side)
- Commit detail pages

### Phase 4: Pull Request System (Weeks 7-10)
- PR creation (auto-generated after build)
- PR overview with diff summary
- Inline code comments (read-only)
- Court code review integration
- Basic approval workflow

### Phase 5: Live Build Dashboard (Weeks 11-12)
- Build status widget with SSE
- Security gate results display
- SBOM viewer
- Log streaming

### Phase 6: Discussion & Deployment Review (Weeks 13-14)
- Threaded discussions (proposal + PR)
- Deployment approval gate
- Risk re-assessment UI
- Post-deployment monitoring

### Phase 7: Polish & Hardening (Weeks 15-16)
- Performance optimization
- Accessibility audit
- Security hardening (mTLS option)
- Documentation updates

## Success Metrics

1. **Transparency**: 100% of SDLC phases visible in web portal
2. **Court Engagement**: Code review adoption rate > 80% for medium+ risk skills
3. **User Satisfaction**: \"I understand what's happening\" metric via survey
4. **Security**: Zero bypass of deployment approval gate
5. **Performance**: Build log streaming latency < 1s
6. **Adoption**: Web portal usage > CLI for review workflows

## Related Documentation

- `docs/architecture.md` — North-star architecture, component boundaries
- `docs/PRD.md` — Original SDLC vision (§11)
- `docs/web-dashboard.md` — Dashboard spec (partial implementation)
- `docs/implementation-plan.md` — Phased delivery roadmap
- `docs/prd-deviations.md` — Alignment tracking

## Open Questions

1. Should Court code review be **mandatory** for all skills or only medium+ risk?
2. How many builder iterations allowed before escalation to user?
3. Should deployment approvals have a timeout (auto-reject)?
4. Notification delivery: Dashboard-only or also native OS notifications?
5. Multi-user support: Single-user first or design for multi-user from start?

## Acceptance Criteria

- [ ] Source code tab: Browse all files in skills/ and self/ repos
- [ ] Git history tab: View commits, branches, diffs
- [ ] PR system: Auto-created after build, displays diff + security results
- [ ] Court code review: Inline comments on generated code (read-only Phase 1)
- [ ] Live build: Stream logs during builder pipeline execution
- [ ] Deployment approval: Modal with risk summary, one-click approve/reject
- [ ] All UI actions logged in Merkle audit tree
- [ ] Performance: Page loads < 300ms, SSE latency < 1s
- [ ] Security: All endpoints ACL-protected, CSP enforced
- [ ] Documentation: Updated tutorial showing web-based skill development flow

## Governance Court Review Required

This proposal constitutes a significant expansion of the web dashboard and introduces new workflow concepts (PR system, deployment gates). Per AegisClaw principles:

- [ ] Submit this issue as formal proposal
- [ ] Court review by all 5 personas (CISO, Coder, SecurityArchitect, Tester, UserAdvocate)
- [ ] Weighted consensus approval
- [ ] Implementation proposal with detailed security analysis

---

**Priority**: High  
**Complexity**: High (multi-phase, 16 weeks estimated)  
**Risk**: Medium (significant UI expansion, new approval gates)  
**Dependencies**: Phases 0-1 of implementation-plan.md (complete)  
**Labels**: enhancement, web-dashboard, sdlc, governance-court, security, ux

https://github.com/PixnBits/AegisClaw/tasks/fe7972ee-8c48-43a7-a099-2726477c1ebf