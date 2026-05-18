# Real Firecracker Store VM - Migration Status

**Status**: Real Firecracker path is now the primary direction. In-process mode removed.

## What Has Been Completed

- Real `StoreVMSpec` with persistent volume support.
- Functional vsock handler in guest that routes to real stores.
- `RemoteClient` with actual vsock communication.
- `launchStoreVM` updated to spawn real mode (full Firecracker spawn in progress).
- Persistent data directory handling in guest.

## Remaining Polish / Next

- Full integration of `sandbox.FirecrackerRuntime` to actually start/stop the VM.
- Proper jailer + chroot setup for the Store VM.
- End-to-end testing of request flow (daemon → vsock → guest stores).
- Rootfs build for store-vm.

Phase 2 seam work is solid. Real VM path is advancing well.