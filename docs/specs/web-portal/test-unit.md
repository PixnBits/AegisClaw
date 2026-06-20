# Unit Test Specifications

**Status**: Target State

## Overview

Unit tests should cover pure logic that does not depend on the vsock bridge or real-time infrastructure.

## High-Value Areas

### Sanitization Logic
- Test that secrets, credentials, and internal paths are redacted in traces.
- Test that Markdown sanitization removes dangerous content.
- Test context-aware sanitization (trace vs chat vs proposal).

### Payload Builders / Formatters
- Test construction of STOMP messages and bridge requests.
- Test formatting of pipeline stages and progress indicators.

### State Machines / UI Logic (if any)
- Any pure logic that manages UI state without side effects.

## Anti-Patterns to Avoid
- Do not put bridge calls or STOMP logic in unit tests.
- Do not test full page rendering here (use component or E2E tests).

## Recommended Location

`internal/dashboard/sanitize/..._test.go`
`internal/dashboard/..._test.go` for small pure helpers.