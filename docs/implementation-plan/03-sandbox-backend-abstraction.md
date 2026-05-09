# 03 - Sandbox Backend Abstraction

## Goal
Implement abstract sandbox backend interface with Firecracker (primary) and Docker (fallback) implementations.

## Acceptance Criteria
- SandboxBackend interface defined
- Firecracker backend functional for basic create/start/stop
- Docker backend functional
- Host Daemon can start a dummy VM using either backend
- Integration test for lifecycle

## References
- specs/host-daemon.md
- specs/runtime-architecture.md

## Test
- `aegis vm create test-vm --backend=firecracker` works
- `aegis vm list` shows it