# Phase 3.5 - Restart-on-Failure Added

- `AegisHubMonitor` now tracks consecutive health failures.
- After reaching threshold, it calls `restart()`.
- Basic restart skeleton in place (full VM re-creation TODO).
- Health recovery resets the failure counter.

Restart logic is now active (can be hardened further with full re-launch).