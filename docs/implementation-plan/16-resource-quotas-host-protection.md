# 16 - Resource Quotas & Host Protection

**Goal**: Implement resource quotas, limits, and host protection mechanisms to prevent resource exhaustion and protect the underlying host from compromised sandboxes.

## Why This Matters
From `docs/specs/additional-requirements-and-gaps.md`:
- Resource quotas and host protection are listed as remaining open questions.
- A compromised sandbox must not be able to exhaust host resources (CPU, memory, disk, network).

## Tasks

1. **Define quota system**
   - Per-VM and per-user resource limits (CPU, memory, disk I/O, network bandwidth)
   - Configurable defaults + per-skill overrides
2. **Enforce quotas in sandbox backends**
   - Firecracker: use cgroups + Firecracker resource limits
   - Docker Sandbox: use Docker resource constraints
3. **Host protection mechanisms**
   - Rate limiting on sandbox creation
   - Kill switch for runaway VMs
   - Monitoring + alerting on quota violations
4. **Tests**
   - Quota enforcement tests (attempt to exceed limits → rejected or throttled)
   - Host protection tests (runaway VM is killed)

## Acceptance Criteria
- Resource quotas are enforced across all sandbox backends
- Host is protected from resource exhaustion attacks
- Clear configuration and monitoring

**Dependencies**: Sandbox backend work + monitoring
**Estimated effort**: 2–3 days

**Owner**: TBD
**Status**: Ready to start