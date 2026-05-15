# 15 - User Journey Automation (Golden Rule)

**Goal**: Automate the remaining User Journeys #2–#9 with Playwright E2E + integration tests so every feature meets the **Golden Rule** ("No feature is considered done until its corresponding user journey has automated tests").

## Current State (from deep analysis + gaps)
- Only Journey #1 is fully automated in CI
- Journeys #2–#9 are partial, placeholder, or documentation-only

## Tasks

1. **Define journey test harness**
   - Extend existing Playwright setup in `cmd/aegisclaw/`
   - Create reusable page objects and helpers for common flows
2. **Implement remaining journeys** (priority order):
   - #2: Basic agent chat + tool use
   - #3: Memory + context persistence
   - #4 & #9: Full SDLC / Governance flow (skill proposal → court → builder → skill activation)
   - #7: Autonomy controls
   - #8: Multi-agent team workflows
3. **CI integration**
   - Add all journeys to the test matrix
   - Fail build if any journey fails
4. **Documentation**
   - Update `docs/roadmap.md` and journey specs with test status

## Acceptance Criteria
- All 9 User Journeys have passing automated tests
- Tests run in CI on every push
- Clear failure messages with screenshots/video on failure
- Golden Rule is visibly enforced

**Dependencies**: Core features + dashboard + court + builder must be functional
**Estimated effort**: 4–6 weeks (can be parallelized)

**Owner**: TBD
**Status**: Ready to start (highest long-term priority)