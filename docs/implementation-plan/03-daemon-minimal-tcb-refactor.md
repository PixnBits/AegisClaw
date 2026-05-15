# 03 - Host Daemon Minimal TCB Refactor

**Goal**: Refactor the Host Daemon (`cmd/aegisclaw` or equivalent) to **strictly** match `docs/specs/host-daemon.md` and the Minimal TCB principle in `docs/architecture.md`. The daemon must become the smallest possible trusted component — no business logic, no untrusted data processing, target < 2000 LOC and < 20 MB idle memory.

## Why This Matters (Paranoid Security)
The daemon runs with root/host privileges. Any extra responsibility dramatically increases the attack surface. Per spec, it must **never**:
- Process user messages or LLM output
- Handle secrets
- Make governance decisions
- Execute generated code

Current implementation likely violates this (user concern + gaps in `additional-requirements-and-gaps.md`).

## Tasks

1. **Audit current daemon code**
   - Identify all non-TCB logic (config loading beyond bootstrap, logging beyond minimal, any API serving, tool registry, etc.)
   - Measure current LOC and idle memory.
2. **Move non-TCB responsibilities**:
   - Business logic (e.g., any ReAct loops, worker management, eval harness) → AegisHub or dedicated sandboxes
   - Complex config → AegisHub or Store VM
   - Any secret-related code → Network Boundary VM only
3. **Enforce strict boundaries**:
   - Keep only: sandbox lifecycle (Firecracker/Docker `sbx`), Unix socket management, Ed25519 keypair distribution to VMs, Merkle root signing, basic watchdog.
   - Implement `SandboxBackend` interface cleanly (already partially there).
4. **Add security hardening in daemon**:
   - Capability dropping (drop all but needed for Firecracker/socket)
   - seccomp-bpf filter
   - Static binary compilation (already required)
   - Memory limits and no dynamic deps
5. **Update tests** (from `docs/specs/host-daemon.md`):
   - Minimal Privilege test
   - No Secret Handling test
   - Lifecycle Containment test (daemon crash → all VMs terminated)
   - Memory < 20 MB idle
   - Static binary verification

## Acceptance Criteria
- Daemon binary < 2000 LOC (excluding tests)
- Idle memory < 20 MB
- Passes all 8 test requirements in `docs/specs/host-daemon.md`
- No business logic remains in daemon (verified by code review + grep for forbidden patterns)
- `aegis status` and basic start/stop still work perfectly

**Dependencies**: Can start after basic CLI coverage (01) and directory layout (02)
**Estimated effort**: 2–3 days (high value for security).

**Owner**: TBD
**Status**: Ready to start (highest security priority after CLI basics)