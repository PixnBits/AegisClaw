# AegisClaw v2 Development Roadmap

## Phase 0: Foundations & Testing Infrastructure (2–3 weeks)
**Goal**: Solid, testable base.

- Host Daemon + AegisHub + sandbox lifecycle (Firecracker + Docker)
- Structured logging, correlation IDs, basic tracing
- Safe Mode implementation
- Full integration test framework
- Playwright E2E skeleton
- Automated tests for **User Journey #1**

## Phase 1: Core Runtime & Agent Loop (3–4 weeks)
- Agent Runtime VM + 6-step loop
- Memory VM + Store VM basics
- Network Boundary VM
- Real-time chat UI (thinking steps, tool calls, incremental Markdown streaming + RAIL)
- CLI + basic Web Portal
- Automated tests for **Journeys #2 and #3**

## Phase 2: Governance & SDLC (4 weeks)
- Full Governance Court (7 personas) + Court Scribe
- Builder VM
- End-to-end skill creation / proposal flow
- **Heavy focus**: Automated integration + E2E tests for **Journeys #4 and #9**

## Phase 3: Advanced Features (3–4 weeks)
- Multi-agent team workflows (Journey #8)
- Autonomy controls (Journey #7)
- Monitoring, Court review UI, background tasks
- Automated tests for remaining journeys

## Phase 4: Polish, Security & Release (2–3 weeks)
- Performance + resource limits
- Security review
- Documentation finalization
- Installer improvements

**Core Rule**: No feature is considered done until its corresponding user journey has automated tests.