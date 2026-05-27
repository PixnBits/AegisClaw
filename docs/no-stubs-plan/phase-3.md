# Phase 3: Full Court + Governance Runtime

**Status:** In Progress (Group 1 complete; see execution log below)  
**Priority:** P1  
**Estimated Effort:** 3 weeks  

**Autonomous Execution (Phase 3 Only):** Following No-Stubs-Left Resolution Plan §Phase 3 + approved session plan exactly. Spec citations in every change. Verification-first. Only daemon lifecycle via `make start`/`make stop` (AGENTS.md).  
**Key Specs:** governance-court.md, court-scribe.md, prd/governance-court.md

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

---

## Autonomous Execution Log (Phase 3 Only — docs/lessons-learned branch)

**Session:** 019e6ba9-cc0f-7d60-9470-fda270cb5b40 (Grok 4.3)  
**Started:** 2026-05-27  
**Execution Mode:** Fully autonomous per approved plan in `~/.grok/sessions/.../plan.md`. **Phase 3 only.** No other phases touched.

### Group 0 (Plan + Exploration) — COMPLETE
- Full read-only exploration of no-stubs-left-resolution-plan.md, phase-3.md, governance-court.md (all sections), court-scribe.md, prd/governance-court.md, current court-*/store/orchestrator/CLI/portal code, hubclient, Docker patterns, AGENTS.md, build scripts, tests, fixtures.
- Created detailed session plan.md (only editable file during planning).
- Baseline verification (make test, build-binaries, doctor).
- Atomic commit prepared.
- **Citations:** no-stubs-left-resolution-plan.md:§Phase 3, phase-3.md:3.1-3.4, governance-court.md:§Architecture + §Output Format Requirements + §Test Requirements, court-scribe.md:§Core Principle.

### Group 1: Real Court Persona Logic + Dockerfiles (user starting task #1 + 3.1) — COMPLETE ✅
**Changes (spec-first, zero new stubs):**
- Created `cmd/court-persona/Dockerfile` (exact model of cmd/agent/Dockerfile + persona boot notes + full spec header citations).
- Created `cmd/court-scribe/Dockerfile` (same pattern).
- `cmd/court-persona/main.go`:
  - Adopted `internal/transport/hubclient` (DialUnix/DialVsock, Register, Send, Receive) — eliminated raw `net.Dial` + manual encoder/decoder stub.
  - Deleted `callLLMWithPersona` mock entirely (the hot-path simulation).
  - Added `resolvePersona` (flag > AEGIS_COURT_PERSONA env > /proc/cmdline "aegis.persona=") for single-image 7-persona real VM support.
  - Added paranoid `loadDistributedKey` (prefers orchestrator 0600 ephemeral key; zeroization).
  - Real LLM path: `callRealLLMViaHub` using exact "llm.call" to network-boundary (same contract as agent loop.go:139).
  - Strict parser `parseStructuredCourtResponse` enforcing governance-court.md output format (fail-closed on malformed → Abstain).
  - `analyzeProposal` now takes optional hubClient (real path when present; test-only simulator clearly marked and never used in binary run loop).
  - Updated run loop to hubclient bidirectional + real analyze(hcl) on every review.
- Updated `main_test.go` for new signature (still covers all 7 personas + prompt logic).
- **Citations in code + commit:** governance-court.md §The Seven Court Personas + §Output Format Requirements + §Implementation Guidance + §Test Requirements + §Architecture; court-scribe.md §Communication Flow (Court pulls from Store directly) + §Security Requirements; agent-runtime.md §Communication (llm via boundary); prd/governance-court.md.

**Verification (after edits, per plan + AGENTS.md):**
- `go build ./cmd/court-persona` + `make build-binaries` → ✓ success (court binaries updated).
- `go test ./cmd/court-persona -run 'TestSign|TestPersona|TestUnique'` + full package tests → ✓ all PASS.
- `./bin/aegis doctor` (baseline) → ✓ (expected "daemon not running" warnings; no regressions).
- No surface mocks remain in the Court execution path inside the binary.

**Commit (atomic):** "phase3: Group 1 real 7-persona logic + Dockerfiles (governance-court.md §Output Format Requirements + §Architecture, court-scribe.md §Communication Flow, no-stubs-plan/phase-3.md 3.1, AGENTS.md, approved session plan)"

**phase-3.md DoD progress:** 3.1 Court Persona VMs — substantial (real logic + images ready; orchestrator launch in Group 3).

**Ready for "continue" → Group 2 (Scribe audit + Merkle decisions).**
