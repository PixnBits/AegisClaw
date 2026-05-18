# Daemon Test Backlog & Progress Tracker

**Purpose**: Track concrete test scenarios, fuzz targets, and verification items for the Host Daemon.

Use this to gauge progress during Phase 5 and beyond.

## Status Legend
- [ ] Not started
- [x] Done / Implemented
- [~] Partial / In Progress

---

## 1. Unit & Integration Test Scenarios

### Lifecycle Containment
- [x] Signal handler registration (`setupLifecycleContainment`)
- [x] `AegisHubMonitor.Stop()` safety
- [x] Stale VM cleanup on startup
- [ ] Full end-to-end: Kill daemon → verify all VMs terminated (integration)
- [ ] Restart-on-failure behavior under health check failure

### Secure Socket & Authorization
- [x] Socket file created with `0600` permissions
- [x] Parent directory created with `0700` permissions
- [x] Stale socket removal handling
- [x] `withAuthorizedCaller` / `authorizeCaller` existence
- [ ] Authorization matrix tests (different peer UIDs)
- [ ] Socket connection rejection for unauthorized callers

### Hardening Functions
- [x] `dropCapabilities` does not panic
- [x] `applySeccompFilter` does not panic
- [ ] Verify actual capabilities are dropped (where testable)
- [ ] Verify seccomp filter is active after application

### Seams & Contracts
- [x] `AegisHubClient` interface check
- [x] `ToolRegistryClient` interface check
- [ ] Mock-based contract tests for `AegisHubClient`
- [ ] Contract tests for `SandboxBackend` interface

### Policy & Regression Guards
- [x] Explicit "No Business Logic" test
- [x] Explicit "No Secret Handling" test
- [ ] CI-enforced grep rules for forbidden patterns (business logic, secrets, governance)

---

## 2. Fuzz Testing Targets (High Priority)

- [ ] Unix socket message parsing / request handling
- [ ] Authorization logic (peer credential validation)
- [ ] Any deserialization from CLI/TUI clients
- [ ] VM specification / config parsing paths
- [ ] Key distribution message formats

**Suggested starting point**: Fuzz the main socket request handler and `authorizeCaller`.

---

## 3. Build-time & CI Checks

- [x] `go test ./...` runs in CI (covers daemon tests)
- [x] Static binary build target (`make build-static`)
- [ ] Automated idle memory measurement in CI
- [ ] LOC budget check / warning in CI
- [ ] `gosec` / static analysis run in CI
- [ ] `govulncheck` in CI

---

## 4. Advanced / Future Techniques

- [ ] Chaos / Fault Injection tests for lifecycle containment
- [ ] Golden file tests for expected permission/hardening state
- [ ] Property-based tests for authorization rules
- [ ] Threat-model-derived test cases

---

## Current Progress Summary (as of latest update)

**Unit/Integration Tests**: Strong start (many core hardening checks implemented)
**Fuzz Testing**: Not started yet
**Build/CI Checks**: Mostly covered
**Advanced Techniques**: Planning stage

This backlog should be reviewed and updated regularly.