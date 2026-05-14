# Phase 0: Foundations & Testing Infrastructure

## Goal
Build a solid, testable base with automated testing from day one.

## Detailed Tasks

### 0.1 Host Daemon Skeleton + Unix Socket
- Create minimal Go Host Daemon
- Implement `aegis start` (sudo), `aegis status`, Unix socket at `~/.aegis/daemon.sock`
- `aegis doctor` command
- **Tests**: Full integration test for start/status

### 0.2 Sandbox Backend Abstraction
- Define `SandboxBackend` interface
- Firecracker + Docker implementations
- Basic VM lifecycle
- **Tests**: Lifecycle integration tests

### 0.3 Minimal AegisHub
- JSON message routing
- vsock communication
- **Tests**: Round-trip tests

### 0.4 Safe Mode
- `aegis safe-mode enable/disable`
- Kill agents, block new ones
- **Tests**: Containment verification

### 0.5 Observability
- Structured JSON logs + trace_id
- `aegis logs` command

### 0.6 Testing Infrastructure
- Integration test harness
- Playwright E2E setup
- Automate User Journey #1
- CI pipeline

**Acceptance Criteria for Phase 0**
- All User Journey #1 fully automated
- System can start and run basic chat test
- 80%+ test coverage on new code