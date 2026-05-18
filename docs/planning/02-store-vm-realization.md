# Phase 2+: Real Firecracker Store VM Implementation (Started)

**Status**: Real Firecracker Store VM work has begun.

## What Was Started

- Added `internal/sandbox/store_vm_spec.go` with `StoreVMSpec` and defaults.
- Enhanced `cmd/store-vm/main.go` to act as a real vsock server (listens on vsock, handles basic JSON requests).
- Uses `vsock.Listen` and basic request routing.

## Next Steps for Real Store VM

1. Build minimal rootfs for store-vm.
2. Wire actual request routing in `handleConnection` to call real store methods.
3. Update `launchStoreVM` in daemon to spawn real Firecracker VM using the spec.
4. Make remote client in `internal/store/remote` actually connect over vsock.
5. End-to-end test: daemon launches Store VM → communicates via vsock.

Phase 2 seam work is complete. This is the realization phase.