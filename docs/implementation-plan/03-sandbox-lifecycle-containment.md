# 03 - Sandbox Lifecycle Containment & Watchdog

**Goal**: Ensure the Host Daemon is the **sole owner** of sandbox lifecycle (start/stop/monitor) with iron-clad containment. If the daemon crashes or is killed, **all** running microVMs/sandboxes must be forcibly terminated. Strengthen the watchdog for AegisHub, Store VM, and Network Boundary VM.

## Paranoid Security Rationale
Per `docs/architecture.md` and `docs/specs/host-daemon.md`:
- Daemon is the only component allowed to launch sandboxes with host privileges.
- Compromised sandbox must never affect the daemon or other sandboxes.
- Lifecycle containment is a core defense-in-depth control.

## Tasks

1. **Implement strict lifecycle ownership**:
   - All `Firecracker` / `sbx` start/stop calls must go exclusively through the daemon's `SandboxBackend`.
   - Remove any direct sandbox calls from AegisHub, agents, or other components.
2. **Crash containment mechanism**:
   - On daemon shutdown/crash: enumerate all known VM IDs (from internal registry) and issue immediate `kill -9` + Firecracker API shutdown.
   - Use a dedicated cleanup goroutine + signal handlers (SIGTERM, SIGINT, SIGKILL).
   - Add `daemon --force-clean` flag for manual recovery.
3. **Watchdog enhancements**:
   - Heartbeat monitoring for AegisHub, Store VM, Network Boundary VM (via vsock or Unix socket pings).
   - On missed heartbeat: log + attempt restart or safe-mode escalation.
   - Integrate with audit log (Merkle-signed event).
4. **Tests** (mandatory per spec):
   - Lifecycle Containment test: Kill daemon → verify zero running VMs within 5 seconds.
   - Watchdog test: Simulate AegisHub crash → daemon detects and recovers/logs.
   - Sandbox isolation test: Compromised sandbox cannot kill other VMs or daemon.

## Acceptance Criteria
- Daemon crash always results in complete sandbox termination (no orphaned VMs).
- Watchdog covers the three critical components (AegisHub + Store + Network Boundary).
- All new behavior covered by automated integration tests.
- No sandbox can influence daemon lifecycle.

**Dependencies**: Builds directly on 02 (daemon refactor).
**Estimated effort**: 1–2 days.

**Owner**: TBD
**Status**: Ready after 02