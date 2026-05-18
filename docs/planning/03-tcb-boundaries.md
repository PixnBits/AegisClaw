# Phase 3.3 Update

**AegisHub communication is now mandatory**.

- Real vsock-based `AegisHubClient` is wired by default in `initRuntime()`.
- Stub / in-process fallback has been removed.
- Multiple chat and session handlers have been converted to proxies that forward to AegisHub.
- The Host Daemon no longer executes chat orchestration or tool dispatch logic directly for these paths.

This significantly reduces the control-plane surface in the privileged daemon.