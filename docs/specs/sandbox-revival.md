# Sandbox Revival & Adaptation PRD

**Status:** Draft / Initial
**Branch:** `feat/sandbox-revival`
**Related old commit:** `d4c0feb77af8cdeeded348dc806bbaeedfdea76a` (pre-rewrite `internal/sandbox`)
**Date:** 2026-06-23

## Background

Prior to the major architecture rewrite (merged into `main`), AegisClaw included a `internal/sandbox` package with a rich implementation for secure microVM sandboxes. Key artifacts from that era (explored via the GitHub connector at the referenced commit):

- `spec.go`: `SandboxSpec`, `SandboxState`, `Resources`, `NetworkPolicy` (with `NoNetwork`, `DefaultDeny`, `AllowedHosts/Ports/Protocols`, `EgressMode` = `proxy` | `direct`), strong validation logic (regex for names/secrets, hostname/FQDN/IP/CIDR checks, SNI-aware proxy mode).
- `netpolicy.go` + tests: detailed network isolation and egress control.
- `manager.go`, `orchestrator.go`, `registry.go`, `snapshot.go`, `cleanup.go`: higher-level lifecycle, VM registration, state snapshots, and teardown.
- VM-specific specs: `aegishub_vm_spec.go`, `store_vm_spec.go`.
- `firecracker.go` (large) and tests.

The rewrite produced the current clean, interface-driven design in `internal/sandbox/`:
- `types.go`: `VMConfig`, `NetworkConfig` (with `EgressViaBoundary`, `BoundaryEgressAddr`, `BoundarySkillID`, `ExposedPorts`, `VsockPort`), `Backend` interface (`Start`/`Stop`/`Status`/`List`/`Cleanup` + `BootPhases`), `VMInfo`, `Status`.
- Backends: `firecracker.go`, `docker.go`, `factory.go`, `backend_*.go`, plus `guest_key_inject.go`, `rootfs_linux.go`.
- Integration with `internal/runtime/orchestrator.go`, host daemon TCB responsibilities (key generation/handoff, zeroing), threat model, vsock channels, and Network Boundary (7.1+).

Many isolation and network concepts survived in evolved form (e.g., boundary egress routing), but some validation rigour, `NoNetwork` strong isolation, proxy-mode SNI enforcement details, snapshot/ registry patterns, and VM-specific spec definitions appear reduced or refactored away.

## Objectives

Create a feature branch to **revive and adapt** the valuable elements of the legacy sandbox into the post-rewrite architecture without regressing the new clean interfaces, security boundaries, or performance.

Primary goals:
1. **NetworkPolicy revival & enhancement**: Port/adapt the strict validation, `NoNetwork` option (pure vsock isolation for court reviewers etc.), `EgressMode` proxy/direct semantics, and SNI/proxy enforcement patterns. Align or merge with current `NetworkConfig.EgressViaBoundary`.
2. **Validation & safety**: Restore or improve the spec validation logic (names, resources, paths, secrets refs, network rules) as reusable helpers or in `VMConfig`.
3. **Lifecycle & observability**: Evaluate reintroducing snapshot support, improved registry semantics, or cleanup hooks if they provide value beyond current `orchestrator` and boot metrics.
4. **VM-specific specs**: Recover or modernise `aegishub_vm_spec` / `store_vm_spec` patterns as typed configs or examples that plug into the generic `Backend`.
5. **Documentation & alignment**: Ensure everything maps cleanly to existing specs (`threat-model.md`, `host-daemon.md`, `security-boundaries.md`, `microvm-observability.md`, `network-boundary.md`, `builder-vm.md`, `store-vm.md`, `aegishub.md`).

Non-goals:
- Full code revert or duplication of the old package.
- Changing the `Backend` interface surface unless a clear, minimal extension is justified.
- Breaking existing Firecracker/Docker VM startup paths or court-persona / store flows.

## Scope & Deliverables

### Phase 1 (this branch start)
- [x] Create `feat/sandbox-revival` branch from `main`.
- [ ] Initial PRD/spec (`docs/specs/sandbox-revival.md` — this document).
- [ ] Side-by-side audit of old `spec.go`/`netpolicy.go` vs current `types.go` + backend implementations.
- [ ] Identify exact gaps (e.g., `NoNetwork` equivalent, proxy egress validation).

### Phase 2
- Prototype adapted `NetworkPolicy` type or extension to `NetworkConfig`.
- Port validation helpers; add tests.
- Update `firecracker.go` (and Docker if relevant) to honour new/revived controls.
- Add or update VM-specific examples/specs.
- Refresh related docs (cross-links, threat model updates if needed).

### Phase 3
- Integration testing (existing e2e + new sandbox-specific).
- Security review / paranoid audit (per project norms).
- Merge to `main` via PR with reviewers.

## Success Criteria
- Equivalent or stronger network isolation options available and enforced.
- No increase in attack surface; all changes pass threat-model review.
- `aegis` commands and web-portal continue to work unchanged for existing flows.
- Clear, maintainable code with good test coverage.
- Updated specs that future developers can follow.

## Open Questions
- Is `NoNetwork` still required, or does `EgressViaBoundary` + vsock-only cover the court-reviewer use case?
- Should snapshot/ registry be revived as separate concerns, or are they now handled in `internal/collab` / `internal/runtime`?
- Any performance or boot-time impact from added validation?
- How to handle migration for any persisted VM state or configs?

## References
- Old sandbox: https://github.com/PixnBits/AegisClaw/tree/d4c0feb77af8cdeeded348dc806bbaeedfdea76a/internal/sandbox
- Current: `internal/sandbox/`, `internal/runtime/orchestrator.go`, `cmd/aegishub/`, `docs/specs/`
- Related PRs from rewrite era (see git history / GitHub PRs #50+).

---
*Initial document created via GitHub connector on `feat/sandbox-revival`. Next steps: detailed code diff exploration and prototype.*
