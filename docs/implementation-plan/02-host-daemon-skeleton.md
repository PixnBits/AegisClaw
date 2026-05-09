# 02 - Host Daemon Skeleton

## Goal
Implement the minimal Host Daemon with Unix socket communication and basic lifecycle commands.

## Acceptance Criteria
- `aegis start` and `aegis status` work
- Unix socket created at `~/.aegis/daemon.sock`
- Basic privilege model implemented

## References
- `../specs/host-daemon.md`
- `../specs/safe-mode.md`

## Test Requirements
- Unit tests for socket communication
- Integration test that starts and stops the daemon