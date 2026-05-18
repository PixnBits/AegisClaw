# Phase 2: Store VM Realization Plan

**Status**: Phase 2.6 completed (Contract & Launch). Moving to 2.7.
**Date**: May 17, 2026

## Goal
Complete realization of the Store VM:
- Host Daemon has **zero ownership** of persistent stores.
- Dedicated Store microVM owns all state.
- Clean transition path from in-process to remote (vsock).

## Current State
- `StoreVM` interface + `NewStoreVM()` fully encapsulates creation.
- `LocalStoreVM` removed (unexported `storeVM`).
- Daemon only interacts via the interface.

## Phase 2.6 Outcome (Completed)
- Clear Store VM responsibilities defined.
- Host Daemon vs Store VM boundary documented.
- Launch & lifecycle responsibilities outlined (modeled after AegisHub).
- `launchStoreVM` added as future core daemon responsibility.

## Phase 2.7: Scaffold Minimal Store VM Binary + Rootfs (Next)

**Tasks**:
1. Create `cmd/store-vm/main.go` skeleton.
2. Reuse store initialization logic from `internal/store`.
3. Add basic vsock listener stub.
4. Define minimal Firecracker spec (reuse `internal/sandbox`).
5. Make it buildable and launchable from daemon later.

**Principles**:
- Keep binary small and focused.
- Same `Store` interface must work.
- Prepare for vsock from day one.

## Full Roadmap
1. Phase 2.6 (Done): Contract + Launch pattern.
2. Phase 2.7: Minimal Store VM binary + rootfs.
3. Phase 2.8: vsock protocol + remote client.
4. Phase 2.9: Dual-mode support in `NewStoreVM()`.
5. Phase 2.10: Daemon launch + monitoring integration.
6. Phase 2.11: Docs, tests, cleanup.

**Next**: Start Phase 2.7 scaffolding.