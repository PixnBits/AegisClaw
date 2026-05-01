# cleanup.go

## Purpose
Provides best-effort cleanup routines for sandbox resources that may be left behind by abnormal process termination, host crashes, or failed sandbox lifecycle operations. Cleanup is idempotent — repeated calls are safe and will not return errors for already-absent resources. This file ensures that TAP network devices, nftables tables, chroot directories, and vsock sockets are fully removed when a sandbox is destroyed or when the daemon restarts after a crash.

## Key Types and Functions
- `CleanupSandbox(id string, cfg RuntimeConfig) error`: removes all host-side resources for the named sandbox: TAP device (`fc-<id>`), nftables table (`aegis_<id>`), chroot directory under `ChrootBaseDir`, and the sandbox state directory under `StateDir`
- `CleanupAllSandboxes(cfg RuntimeConfig) error`: scans `StateDir` for all known sandbox state directories and calls `CleanupSandbox` on each; used at daemon startup to recover from crashes
- TAP cleanup: calls `ip link delete fc-<id>` via `os/exec`; ignores "not found" errors
- nftables cleanup: calls `nft delete table` for the sandbox table; ignores "no such table" errors

## Role in the System
Called by `FirecrackerRuntime.Delete` for normal teardown and by the daemon's startup sequence via `CleanupAllSandboxes` for crash recovery. Ensures the host is left in a clean state after any sandbox exits.

## Dependencies
- `os/exec`: `ip` and `nft` command invocation
- `os`, `path/filepath`: state directory enumeration and removal
- `internal/sandbox`: `RuntimeConfig` type
