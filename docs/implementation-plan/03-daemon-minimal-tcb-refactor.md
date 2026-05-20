# Task 03: Daemon Minimal TCB Refactor — Implementation and Test Specification

**Status**: Implementation completed (May 2026). This document is the **authoritative test-specification anchor** for Host Daemon TCB work (Phases 3–4), directory layout integration, and related hardening. It stays in sync with [docs/specs/host-daemon.md](../specs/host-daemon.md), [docs/planning/03-tcb-boundaries.md](../planning/03-tcb-boundaries.md), and [docs/planning/phase4-hardening.md](../planning/phase4-hardening.md).

**Related planning**

- [docs/planning/03-tcb-boundaries.md](../planning/03-tcb-boundaries.md) — responsibility migration and attack-surface analysis
- [docs/planning/phase3-summary.md](../planning/phase3-summary.md)
- [docs/planning/phase4-hardening.md](../planning/phase4-hardening.md)
- [docs/planning/phase5-verification.md](../planning/phase5-verification.md)
- [docs/planning/daemon-testing-strategy.md](../planning/daemon-testing-strategy.md) — CI and local matrix
- [docs/planning/daemon-test-backlog.md](../planning/daemon-test-backlog.md) — prioritized gaps from this matrix

**Adjacent implementation plans** (do not duplicate their full test lists; link here for traceability)

- [02-directory-layout.md](02-directory-layout.md) — filesystem layout and secure defaults (including removal of legacy path migrations on this branch)
- [04-unix-socket-hardening.md](04-unix-socket-hardening.md) — peer credentials, rate limits, audit on connect
- [06-sandbox-lifecycle-containment.md](06-sandbox-lifecycle-containment.md) — kill containment, watchdog, orphaned VM prevention

---

## 1. Scope: what “minimal TCB” means

The Host Daemon intentionally retains only:

- **VM lifecycle**: start/stop/monitor sandboxes (Firecracker / `sbx` abstraction)
- **Privileged Unix socket**: CLI and TUI IPC (hardened permissions)
- **Cryptographic bootstrap**: Ed25519 key distribution to microVMs, Merkle root signing for audit
- **Watchdog**: critical components (AegisHub, Store VM; Network Boundary VM per spec)

**Moved out of the daemon execution path** (proxy or AegisHub / Store VM ownership): chat, sessions, workers, EventBus business logic, tool registry execution, governance decisions, and persistent store construction in-process. See the migration table in [03-tcb-boundaries.md](../planning/03-tcb-boundaries.md).

---

## 2. Requirements traceability matrix

**Columns**

- **Requirement** — normative source
- **Test intent** — what must be proven
- **Type** — per [docs/testing-standards.md](../testing-standards.md)
- **Location** — primary test or gate (or **gap**)
- **CI tier** — `PR` = default `make test`; `INT` = `make test-integration` or tagged; `REL` = release/manual; `N/A` = documented exception

### 2.1 [docs/specs/host-daemon.md](../specs/host-daemon.md) — Test Requirements

| Requirement | Test intent | Type | Location | CI tier | Status |
|-------------|-------------|------|----------|---------|--------|
| Minimal privilege | Capability drop and bounding set invoked without panicking; stricter proofs where CAPs available | Unit / smoke | `cmd/aegisclaw/daemon_tcb_test.go` (`TestHardening_CapabilitiesDropCalled`, `TestHardening_CapabilityBoundingSetApplied`) | PR | **Partial** — environment-dependent; skips/errors logged, not full cap inventory assertion |
| No secret handling | Daemon does not initialize vault/court/build paths as part of minimal runtime init documented for TCB | Unit / structural | `cmd/aegisclaw/daemon_tcb_test.go` (`TestDaemonDoesNotInitializeForbiddenComponents`, `TestNoNonTCBInitializations`) | PR | **Partial** — several tests rely on compile-time removal + weak runtime assertions; strengthen with explicit field/build tags where possible |
| Keypair isolation | Private keys for Merkle and VM contracts do not leak across packages / wrong verify paths | Unit | `internal/audit/merkle_test.go` (`TestMerkleLog_WrongKeyDetection`, tamper tests) | PR | **Partial** — proves Merkle crypto layer, not full end-to-end “key never leaves VM” distribution (see backlog) |
| Lifecycle containment | On daemon exit, monitors stop cleanly; future: no orphaned VMs | Unit / INT | `cmd/aegisclaw/lifecycle_integration_test.go`; `cmd/aegisclaw/daemon_test.go` (monitor threshold) | INT / PR | **Partial** — integration file today exercises monitor structs, not full Firecracker teardown (see [06](06-sandbox-lifecycle-containment.md)) |
| Memory usage (under 20 MB idle) | Idle RSS within budget on reference Linux | Manual / benchmark | **gap** | REL | **Missing** — needs bounded benchmark job |
| Static binary | Produced binary is statically linked | Build gate | `Makefile` `build-static` target | REL | **Implemented** |
| Sandbox isolation | Default-deny and policy tables for sandboxes | Unit | `internal/sandbox/security_test.go`, `netpolicy_test.go`, `firecracker_test.go` | PR | **Partial** — strong unit coverage; cross-VM escape not automated here |
| Audit root signing | Merkle append, chain, tamper detection | Unit | `internal/audit/merkle_test.go` | PR | **Partial** — log crypto; **gap** for daemon “sign at interval” loop and integration with live audit dir |
| Unix socket hardening | Modes after creation, owner-only socket set, symlink resistance on runtime dir, peer UID on Linux | Unit | `cmd/aegisclaw/daemon_test.go` (`TestCreateSecureSocket_PermissionAfterCreation`); `internal/paths/paths_test.go`; `internal/api/server_peeruid_linux_test.go` | PR | **Partial** — peer UID test is Linux-only; full SO_PEERCRED allow-list and rate limits in [04](04-unix-socket-hardening.md) backlog |

### 2.2 Phase 4 hardening ([phase4-hardening.md](../planning/phase4-hardening.md))

| Hardening area | Test intent | Type | Location | CI tier | Status |
|----------------|-------------|------|----------|---------|--------|
| Lifecycle containment (signals + cleanup) | Aggressive termination paths exist and hooks run | Unit / smoke | Hardening tests in `daemon_tcb_test.go`; daemon lifecycle code review | PR | **Partial** — no full subprocess “kill daemon → zero VMs” yet |
| Capability dropping | `dropCapabilities` / bounding set exercised | Unit | `daemon_tcb_test.go` | PR | **Partial** |
| seccomp-bpf | Filter install hook runs | Unit | `TestHardening_SeccompFilterHook` | PR | **Partial** — non-fatal in many envs |
| Static binary | Same as host-daemon row | Build | `make build-static` | REL | **Implemented** |
| Unix socket permissions | `0700` runtime dir pattern, `0600` socket ownership helper | Unit | `internal/paths/paths_test.go` (`TestSetRuntimeSocketOwnerUsesOwnerOnlyMode`, runtime dir tests) | PR | **Implemented** for path helpers; end-to-end bind still see 04 |

---

## 3. TCB API surface regression suite

**Goal**: Non-TCB RPC paths must stay **removed, proxied, or explicitly stubbed** with **stable, safe errors** (no accidental reintroduction of business logic in the daemon).

**Invariants** (from [03-tcb-boundaries.md](../planning/03-tcb-boundaries.md)):

- No in-process construction of ProposalStore, MemoryStore, EventBus, WorkerStore, etc. on the daemon init path.
- Extended handlers that remain for compatibility return documented “not in minimal TCB” errors rather than undefined behavior.

**Current automated checks**

| Concern | Location | Notes |
|---------|----------|--------|
| Forbidden init / structural TCB | `cmd/aegisclaw/daemon_tcb_test.go` | Several tests are **documentation-heavy** (compile-time guarantees). **Strengthen**: assert specific handler registrations or RPC error strings via a test-only introspection hook where architecturally allowed. |
| CLI↔daemon stub / TCB denial strings | `cmd/aegisclaw/cli_api_contract_test.go` (`TestDaemonAPI_EndpointContract`, `isExplicitStubError`) | Treats stable “removed from minimal Host Daemon TCB”, vault disabled, and proxy-unavailable errors as explicit denials (**DB-07** partial). |
| Authorization edge | `cmd/aegisclaw/daemon_test.go` `TestWithAuthorizedCaller_EmptyAction` | PR |
| Panic recovery in IPC | `internal/api/server_test.go` | PR |

**Recommended additions** (tracked in [daemon-test-backlog.md](../planning/daemon-test-backlog.md)): extend the contract table for every deprecated handler ID not yet listed; optional golden list next to handler registration when `registerCoreTCBHandlers` is restored beyond a stub.

---

## 4. Directory layout and branch regression ([02-directory-layout.md](02-directory-layout.md))

Legacy path migration was removed: configs and paths must always materialize with **secure defaults** (no silent rewrite from old layouts in daemon startup).

| Requirement | Test intent | Type | Location | CI tier | Status |
|-------------|-------------|------|----------|---------|--------|
| Single `~/.aegis` root for user data paths | Defaults under home root | Unit | `internal/config/directory_layout_test.go` | PR | **Implemented** |
| Socket not under `~/.aegis` on Linux | `DefaultConfig` socket path prefix | Unit | `internal/config/directory_layout_test.go` | PR | **Implemented** |
| Secure directory modes + symlink rejection | `EnsureSecureDirectories`, `VerifySensitiveDir`, runtime dir | Unit | `internal/paths/paths_test.go` | PR | **Implemented** |
| Vault access hardening | Loose perms / symlink index rejected | Unit | `internal/vault/directory_layout_security_test.go` | PR | **Implemented** |
| No legacy migration path | No silent rewrite of paths on load; fresh install matches `DefaultConfig()` | Unit | `internal/config/load_migration_regression_test.go` | PR | **Partial** — behavioral regression (**DB-08**); optional static guard for removed symbol names still welcome |

---

## 5. Design principles for future tests

- **Defense in depth**: pair negative tests (wrong perms, symlink, empty action) with contract tests (stable IPC errors).
- **Determinism**: environment-dependent tests use `t.Skip` with a clear reason or live under `-tags=integration`.
- **Observability**: where specs require audit events (connection denied, unsafe dir), assert log fields or audit records in tests.
- **Single matrix**: this file owns Host Daemon TCB rows; [04](04-unix-socket-hardening.md) and [06](06-sandbox-lifecycle-containment.md) reference this section for IDs to avoid drift.

---

## 6. Historical completion note

Phase 3 (handler extraction + AegisHub strengthening) and Phase 4 (host hardening: caps, seccomp, static build, socket defaults, lifecycle hooks) were implemented in code. **Test maturity** lags in the areas called out as **Partial** or **Missing** above and in [docs/specs/additional-requirements-and-gaps.md](../specs/additional-requirements-and-gaps.md); use this matrix to drive PRs without re-litigating scope.
