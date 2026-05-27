# Phase 3: Full Court + Governance Runtime

**Status:** Not Started  
**Priority:** P1  
**Estimated Effort:** 3 weeks

## Goal
Implement real 7-persona Court microVMs with voting, decision recording, and feedback into running agents.

## Key Specifications
- `docs/specs/governance-court.md`
- `docs/specs/court-scribe.md`
- `docs/prd/governance-court.md`

## Definition of Done
- [ ] 7 Court personas run as real Firecracker microVMs
- [ ] Voting produces tamper-evident, signed decisions
- [ ] Court decisions can revoke scopes or terminate agents
- [ ] Court Scribe records full audit trail
- [ ] Agent Runtime respects Court decisions in real time
- [ ] No simulation or fixture data in the Court path

## Detailed Tasks

### 3.1 Court Persona VMs (Week 1)
- Create `cmd/court-persona/main.go` with real persona logic
- Implement 7 distinct personas (security, ethics, legal, etc.)
- Wire vsock to AegisHub for proposal review

### 3.2 Voting & Decision Engine (Week 1–2)
- Implement real voting protocol with Merkle-signed decisions
- Add threshold and consensus rules per spec
- Wire decisions back to Agent Runtime (scope revocation, termination)

### 3.3 Court Scribe Integration (Week 2)
- Complete `cmd/court-scribe/main.go` with full audit logging
- Record every proposal, vote, and decision with timestamps and signatures
- Expose `/api/court/decisions` with real data

### 3.4 End-to-End Governance Flow (Week 3)
- Full flow: skill proposal → Court review → vote → decision → agent enforcement
- Update E2E tests to cover real Court path
- Remove all simulation code from CLI and Portal

## Success Criteria
When this phase is complete, Court decisions are real, auditable, and immediately affect running agents — with zero simulation.
