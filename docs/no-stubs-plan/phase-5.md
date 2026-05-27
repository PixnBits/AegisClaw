# Phase 5: Complete Web Portal + Full E2E + Final Polish

**Status:** Not Started  
**Priority:** P2  
**Estimated Effort:** 2 weeks

## Goal
Wire all remaining Web Portal features, complete 100% E2E automation for all 9 journeys, and achieve "no stubs left" certification.

## Key Specifications
- `docs/specs/web-portal.md`
- `docs/specs/web-portal-screens.md`
- `docs/testing-standards.md`
- `docs/specs/additional-requirements-and-gaps.md`

## Definition of Done
- [ ] Canvas, full streaming chat with Markdown, proposal detail with round feedback, memory search, approvals all fully functional
- [ ] All 9 user journeys have complete, automated E2E tests (including failure + recovery)
- [ ] `additional-requirements-and-gaps.md` shows zero open items
- [ ] ≥85% overall test coverage
- [ ] Zero remaining "limited mode", "surface-only", or stub disclaimers in user-facing paths

## Detailed Tasks

### 5.1 Web Portal Completion (Week 1)
- Wire remaining handlers (`git.*`, `workspace.*`, `dashboard.skills`, approvals)
- Implement Canvas view, full streaming chat, memory search
- Add stable `data-testid` attributes for all new screens
- Complete Playwright coverage for new features

### 5.2 Full E2E Automation (Week 1–2)
- Expand `e2e/journeys.spec.js` to cover all 9 journeys end-to-end
- Add failure + recovery scenarios for every journey
- Integrate with real runtime (after Phase 1–4)
- Make `make test-e2e` consistently green in CI

### 5.3 Final Polish & Certification (Week 2)
- Run full "no stubs left" audit across codebase
- Update `additional-requirements-and-gaps.md` with final status
- Complete threat model review
- Add missing operational scripts (image build, live tests)
- Final `make test` + `make test-chaos` + doctor run

## Success Criteria
When this phase is complete, the project has **zero stubs** and is ready for production v2.1 release.
