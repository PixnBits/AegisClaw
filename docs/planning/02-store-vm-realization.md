# Store VM Realization – Scaffold / Seam (Work in Progress)

**Status**: Initial scaffold pushed. The daemon has the plumbing to spawn a Store VM, but the implementation is still being completed.

## Achieved (Scaffold)

- In-process `StoreVM` facade provides the correct interface for future migration.
- vsock listener and client stubs exist as the protocol seam.
- Persistent volume path support wired in guest (`-data-dir` flag).
- `launchStoreVM` outline exists; real `FirecrackerRuntime` wiring is deferred.
- Remote client parses vsock address; actual request routing is a placeholder.

## Not Yet Complete

- `cmd/store-vm/main.go` handler is a stub (`handleConnection` is a TODO).
- The daemon does not yet spawn a real Firecracker Store VM; the in-process
  fallback is used instead.
- vsock protocol (request/response framing, op routing) needs full implementation.
- End-to-end integration tests are deferred to a follow-up PR.

Remaining work is the functional handler, rootfs, Firecracker wiring, and full testing.