# 05 - Safe Mode Implementation

## Goal
Implement emergency containment (kill switch).

## Acceptance Criteria
- `aegis safe-mode enable` works
- Kills all Agent Runtimes, blocks new ones
- Web Portal shows banner
- `aegis safe-mode disable` restores normal operation

## References
- specs/safe-mode.md

## Test
- Trigger Safe Mode and verify all agents stopped