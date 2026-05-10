# Testing Standards for AegisClaw v2

## Core Requirements

- **Unit Test Coverage**: ≥ 80% for all new code
- **Integration Tests**: All User Journeys (1-9) must have automated integration tests
- **E2E Tests**: Web Portal flows must be covered with Playwright
- **Chaos Testing**: Regular testing of component failures and recovery

## Testing Philosophy

- Test first where possible
- Every feature must have tests before it is considered complete
- Paranoid testing: assume components can fail or be compromised

## Required Test Types

1. Unit tests
2. Integration tests (full system with sandboxes)
3. E2E tests for CLI and Web Portal
4. Security gate tests in Builder VM
5. Safe Mode recovery tests

## CI/CD Requirements

- All tests must pass before merge
- Coverage report generated on every PR

## Related Documents
- docs/implementation-plan/
- docs/specs/*
