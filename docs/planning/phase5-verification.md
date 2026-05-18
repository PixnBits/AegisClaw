# Phase 5: Verification - Expanded Tests

## Test Coverage (cmd/aegisclaw/daemon_test.go)

- `TestSecureSocketCreation` — verifies strict `0600` permissions on socket files.
- `TestLifecycleContainmentSignalHandling` — ensures signal handlers can be registered.
- `TestCapabilityDroppingDoesNotPanic` — hardening functions are callable.
- `TestSeccompFilterApplication` — seccomp can be applied.
- `TestNoObviousSecretPatterns` — reminder + policy test.

These tests provide basic runtime confidence. Full paranoid verification still relies on:
- Code review
- CI static analysis (gosec, etc.)
- Build-time checks (static binary, memory)
