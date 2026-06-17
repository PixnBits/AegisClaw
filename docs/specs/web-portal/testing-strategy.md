# Testing & Contract Test Strategy

**Status**: Target State

## Overview

This document outlines the testing strategy for the Web Portal, with emphasis on E2E coverage, contract testing for real-time and bridge interactions, and stable testability of the UI.

## Goals

- Ensure all major user journeys have reliable automated test coverage.
- Validate real-time behavior (STOMP subscriptions, updates, cleanup).
- Prevent regressions in security boundaries and sanitization.
- Provide stable selectors for tests while allowing design flexibility.

## E2E Test Coverage (Playwright)

High-priority journeys that should have dedicated E2E tests:

- Starting a new task from Home and seeing plan decomposition + channel creation.
- Collaborative work in a channel with proactive agent updates.
- Reviewing and acting on a Court proposal (vote, approve, see rationales).
- Monitoring active work on Dashboard and drilling into traces or Canvas.
- Member management (invite human and specialist agent).
- Real-time updates across multiple tabs/sessions.

Edge cases to cover:
- Empty states and first-time user guidance.
- Long activity feeds and traces (virtualization behavior).
- STOMP disconnection and fallback to SSE.
- High-privilege actions and confirmation flows.

## Contract Testing

- Define and maintain contract tests for STOMP payload shapes (as specified in `real-time-contracts.md`).
- Contract tests for key bridge actions the portal calls.
- These should run as part of CI and fail on breaking changes to payloads or behavior.

## Selector Strategy

- Use stable `data-testid` attributes for critical interactive elements and containers that tests rely on.
- Avoid over-reliance on CSS classes or text content for test selectors (these can change with design or localization).
- Document the list of stable test IDs in the testing documentation.

## Security & Sanitization Testing

- Include tests that verify sensitive data is not leaked in traces, chat, or activity feeds.
- Test rate limiting and input validation behavior on high-impact actions.
- Validate that error messages do not expose internal implementation details.

## Performance & Load Considerations in Tests

- Include some tests with realistic data volumes (long feeds/traces) to catch performance regressions early.
- Monitor test execution time; keep the core E2E suite fast enough to run frequently.

## Implementation Recommendations

- Organize tests by user journey and by view.
- Use page object or component object patterns for maintainability.
- Integrate contract tests into the build pipeline.
- Run a subset of critical E2E tests on every push; full suite on scheduled or release builds.

A strong testing strategy ensures the portal remains reliable, secure, and maintainable as it evolves.