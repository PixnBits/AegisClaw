# Phase 4: Host Daemon Hardening

## 4.1 Lifecycle Containment - Done

- Added signal handling for SIGINT/SIGTERM/SIGQUIT.
- On termination: stops AegisHubMonitor (which stops the VM).
- Added best-effort stale VM cleanup on startup (crash recovery).
- Wired into `initRuntime()`.

This ensures VMs are not left running if the daemon is killed.