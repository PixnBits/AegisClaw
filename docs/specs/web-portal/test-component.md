# Component & Integration Test Specifications

**Status**: Target State

## Overview

Component and small integration tests sit between unit tests and full E2E. They are useful for validating UI behavior with realistic (but mocked) data without requiring a full running daemon + multiple microVMs.

## Recommended Scope

- Rendering of complex views with mocked events (Canvas, activity feed, traces, pipeline overview).
- Interaction flows that involve multiple components (e.g., command bar → plan preview → channel transition).
- State management within a view when receiving real-time events.
- Error and loading state rendering.

## Example Test Ideas

### Canvas View
- Render multiple agents in different stages.
- Verify pipeline progress updates when mock events arrive.
- Test drill-down from agent card to trace view.

### Activity Feed
- Verify proactive agent updates appear with correct styling.
- Test @mention autocomplete behavior.
- Verify proposal events render with correct status badges.

### Trace View
- Test expansion of tool calls.
- Verify sanitization is applied in rendered output.
- Test navigation from trace back to originating channel.

## Mocking Strategy

- Use test doubles for STOMP events and bridge responses.
- Provide realistic but controlled data fixtures.
- Avoid starting real microVMs in these tests.

## Location Suggestion

`internal/dashboard/ui/components/..._test.go` or a dedicated component test directory.