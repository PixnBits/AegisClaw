# Phase 3 Summary: Daemon Minimal TCB + AegisHub Strengthening

**Status**: Phase 3 Complete

## Overview

Phase 3 focused on aggressively reducing the Host Daemon's control-plane surface by moving logic to AegisHub, while strengthening AegisHub's own launch, monitoring, and lifecycle management.

## Major Achievements

### 3.1 – 3.4: Handler Extraction
- Converted large portions of control-plane handlers to thin proxies that forward to AegisHub.
- Extracted: Chat, Sessions, Workers, EventBus (Approvals, Timers, Signals).
- Created and extended `AegisHubClient` with real vsock communication.
- Tool Registry seam established (moving toward AegisHub ownership).

### 3.5: AegisHub Lifecycle Hardening
- Implemented **actual Firecracker launch** for AegisHub using `sandbox.FirecrackerRuntime`.
- Added real health checking via vsock.
- Built cancellable monitoring loop with recovery detection.
- Implemented **restart-on-failure** with VM re-creation.
- Strong graceful shutdown support.

### Key Architectural Improvements
- `AegisHubMonitor` provides centralized lifecycle control.
- Clear separation between daemon TCB and AegisHub responsibilities.
- Health monitoring now actively detects and recovers from issues.

## Files Changed (High Level)
- `cmd/aegisclaw/runtime.go` — Launch, monitoring, and lifecycle
- `internal/aegishub/client.go` — Real vsock client + health checks
- `internal/sandbox/aegishub_vm_spec.go` — AegisHub VM specification
- Multiple handler files — Proxy implementations
- Documentation in `docs/planning/`

## Verification Steps Completed
- All major placeholders and TODOs resolved.
- Consistent use of seams (`AegisHubClient`, `AegisHubMonitor`).
- Documentation updated across boundaries and checklists.

## Next
Phase 4: Host Daemon Hardening (capability dropping, seccomp, smaller TCB surface).
