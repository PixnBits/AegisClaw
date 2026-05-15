# 14 - Advanced Builder Security Gates

**Goal**: Implement full SAST, SCA, secrets scanning, policy-as-code enforcement, health checks, and automatic rollback in the Builder pipeline (beyond current SBOM support).

## Current State
SBOM generation is implemented. Advanced gates (SAST/SCA/policy-as-code/health checks + rollback) are incomplete per gaps analysis.

## Tasks

1. **Integrate security scanners**
   - Add SAST (e.g., gosec, semgrep)
   - Add SCA (dependency vulnerability scanning)
   - Enhance secrets scanning (already partial)
2. **Policy-as-code engine**
   - Define and enforce security policies (e.g., no privileged containers, approved base images)
   - Use OPA or similar for policy evaluation
3. **Health checks + rollback**
   - Post-build health verification
   - Automatic rollback on failure or policy violation
4. **Tests**
   - Pipeline tests with failing SAST/SCA/policy cases
   - Rollback verification tests

## Acceptance Criteria
- Full SAST + SCA + enhanced secrets scanning in every build
- Policy-as-code enforcement with clear violations
- Automatic rollback on failure
- Full alignment with `docs/specs/builder-security-gates.md`

**Dependencies**: Builder pipeline + SBOM work
**Estimated effort**: 3–4 days

**Owner**: TBD
**Status**: Ready to start