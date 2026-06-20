# Security & Sanitization Test Specifications

**Status**: Target State

## Overview

Security and sanitization tests are critical due to the paranoid security model. These tests should verify that sensitive data never leaks to the browser and that input is properly validated.

## High-Priority Areas

### Output Sanitization
- Traces: Verify credentials, internal paths, and raw memory are redacted.
- Chat / Activity Feed: Verify dangerous Markdown or HTML is stripped.
- Proposals: Verify diffs and artifacts do not leak internal information.

### Input Validation
- High-impact actions (approve proposal, cancel agent, invite powerful personas) require confirmation.
- Rate limiting is enforced on sensitive endpoints and STOMP actions.
- Malformed or oversized input is rejected gracefully.

### Bridge Action Restrictions
- Portal only calls actions in the documented allow-list.
- Attempts to call disallowed actions are blocked (and ideally logged on the Host).

### Error Message Hygiene
- Error messages shown to users never contain stack traces, internal paths, or implementation details.

## Recommended Test Types

- Unit tests for sanitization functions (with concrete examples).
- Contract tests that verify the portal never sends disallowed bridge actions.
- E2E spot-checks for critical flows (e.g., viewing a trace after a sensitive tool call).

## Regression Protection

These tests are especially important for catching regressions when new features or agent capabilities are added.