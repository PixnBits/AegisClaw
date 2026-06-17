# E2E Test Specifications (Playwright)

**Status**: Target State

## Overview

E2E tests should be kept **minimal** and focused on high-value user journeys that are difficult to validate at lower layers.

## Recommended E2E Tests (Maximum 6–8 total)

1. **Start task from Home**
   - User enters goal in command bar.
   - Verify plan preview appears.
   - Verify transition to channel with visible harness/pipeline state.

2. **Collaborative work with proactive updates**
   - Agent sends proactive update in channel.
   - Verify it appears correctly in activity feed.

3. **Full Court review flow**
   - Proposal appears in Court.
   - User votes as different personas.
   - Verify rationales and final decision.

4. **Real-time across tabs**
   - Open same channel in two tabs.
   - Make change in one tab.
   - Verify update appears in second tab.

5. **STOMP disconnect + SSE fallback**
   - Simulate STOMP failure.
   - Verify UI continues to receive updates via SSE.

6. **Member management flow**
   - Invite human and specialist agent.
   - Verify grouped member list updates correctly.
   - Remove a member and verify clean state.

## Guidelines

- Use stable `data-testid` attributes.
- Avoid tests that depend on timing of agent microVM startup.
- Prefer explicit waits over fixed sleeps.
- Keep this suite fast enough to run on every significant change.