# Implementation Gaps & Priorities

**Status**: Target State / Living Document

## Purpose

This document consolidates the remaining open questions, considerations, and areas that require additional specification for a high-quality implementation of the Web Portal. It serves as a single reference for implementors and reviewers.

It is intended to be updated as new specifications are created.

## Completed / Well-Covered Areas

The following areas have dedicated target-state specifications:

- Overall portal vision, principles, and page behaviors (`web-portal.md` + per-page specs)
- Real-time communication contracts and STOMP usage (`real-time-contracts.md`)
- Security boundaries, sanitization, input validation, and rate limiting (`security-boundaries.md`)
- Harness / pipeline data model and visibility (`harness-pipeline-data-model.md`)
- Channel collaboration, activity feed, and member management (`channels.md` + `member-management-flow.md`)
- Court / governance flows and adversarial review (`court.md`)
- Dashboard, monitoring, and intervention (`dashboard.md`)
- Canvas / inter-agent pipeline visualization (`canvas.md`)
- Single-agent trace and deep observability (`single-agent-trace.md`)
- SDLC flows: source browser, git history, PR system, build dashboard, deployment gates (`./sdlc-web-portal.md`)
- **Agent customization foundation** (`../agent-customization.md` – expanded with per-agent SETTINGS.yaml, LLM usage metrics model, and individual agents page requirements)

## Remaining Open Areas (Prioritized)

### High Priority (Recommended before major implementation)

1. **Harness Pipeline Data Model – Refinement**
   - More precise schemas for how the PM communicates narrow tasks, stage transitions, and progress to the portal.
   - Event types and payload deltas for real-time updates.
   - Status: Partially covered in `harness-pipeline-data-model.md`; needs tighter integration with STOMP contracts.

2. **Design Tokens & Component Patterns**
   - Official design tokens (colors, typography, spacing, shadows, focus states, loading states).
   - Reusable component patterns (cards, badges, timelines, pipeline indicators, member chips).
   - Dark theme specifics and accessibility (contrast, focus management).
   - Status: Not yet documented in detail.

3. **Performance Targets & Virtualization**
   - Specific targets for concurrent connections, render performance, and trace/feed length before virtualization or pagination is required.
   - Strategy for virtual scrolling in long activity feeds and traces.
   - Memory and CPU budgets for the portal VM under load.
   - Status: Not yet specified.

4. **Per-Agent Settings Editor + LLM Usage Metrics (Individual Agents Page)**
   - Full implementation of the expanded requirements in `../agent-customization.md`:
     - SETTINGS.yaml schema, validation, atomic writes + backup, LoadForAgent precedence; portalbridge get/set + daemon handlers.
     - LLM usage tracking at network-boundary (tokens/model/duration/success from Ollama), store handlers + aggregates (grand/last-hour/today/MTD + by-model), dashboard /api/llm-usage.
     - Exposure: /api/agents + /api/agents/<id>/settings , /api/agents/<id>/metrics ready; STOMP topics + publish helper; frontend list metrics summary + client/editor APIs.
   - Backend collection outside guest (boundary+store), contract tests, e2e fixtures.
   - Security & isolation: metrics/storage in boundary/store/dashboard only; writes validated + via daemon.
   - Status: **Implemented** (feat/agent-settings-and-metrics). See changed files: cmd/network-boundary/*, cmd/store/main.go, internal/workspace/loader.go, internal/dashboard/* (contracts, extended, realtime), web-portal/src/*, e2e/ + tests. Hot-reload notification partial (file load on start); full live UI polish follow-up.

### Medium Priority

5. **Onboarding, Suggestions Engine & Empty States**
   - Exact logic for detecting first-time vs returning users.
   - Rules and data sources for contextual suggestions on Home.
   - Standardized empty state and guidance copy across views.
   - How external signals (news, market data, etc.) are sourced and filtered securely.
   - Status: High-level guidance exists; detailed rules needed.

6. **Export Formats & Compliance Artifacts**
   - Exact data shapes and file formats for Court exports (structured reports, SBOM mapping, regulatory artifacts).
   - How proposal metadata and rationales are serialized for diligence/compliance use cases.
   - Status: Mentioned in `court.md`; needs concrete schemas.

7. **Testing & Contract Strategy**
   - E2E test coverage matrix (which journeys and edge cases require Playwright coverage).
   - Contract tests for STOMP payloads and bridge actions.
   - Strategy for testing real-time behavior reliably.
   - Stable `data-testid` and selector guidelines.
   - Status: Partially implied; needs explicit document.

### Lower Priority / Nice to Have

8. **Detailed Component Interaction Specs**
   - State machines or detailed interaction flows for complex components (Command Bar + plan preview, Grouped Member Management, Proposal voting).
   - Loading, error, and optimistic update patterns.

9. **Member Permission Model Details**
   - Fine-grained permissions for who can invite/remove specific member types.
   - Governance requirements for adding powerful Court or specialist personas.

10. **Internationalization & Accessibility**
   - i18n strategy and requirements.
   - Detailed a11y requirements beyond basic contrast and keyboard navigation.

## Recommended Implementation Order

1. Finalize Harness Pipeline Data Model + integrate with real-time contracts.
2. Define Design Tokens & core component patterns (this unblocks consistent UI implementation).
3. **Implement Per-Agent Settings + LLM Metrics (high value, builds directly on newly expanded spec).**
4. Establish Performance Targets & virtualization approach.
5. Create Testing & Contract Strategy document.
6. Detail Onboarding / Suggestions logic and Export Formats.
7. Address remaining lower-priority items as needed during development.

## How to Use This Document

- Implementors should treat items marked High Priority as blocking for a production-quality release.
- New specifications should be created in this folder and referenced here.
- This document should be updated when gaps are closed.

## Next Steps

The Per-Agent Settings + LLM Metrics item (now #4) and the original high-priority items are the strongest candidates for immediate implementation work on this branch or follow-ups. The expanded `agent-customization.md` provides the target-state definition to drive coding.