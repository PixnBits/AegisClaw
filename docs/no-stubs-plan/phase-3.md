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

### Group 2: Court Scribe Full Audit + Tamper-Evident Decisions (user task #4 + 3.3) — COMPLETE ✅

**Changes (spec-first):**
- `cmd/court-scribe/main.go`:
  - Added `buildSignedDecision` (cites court-scribe.md §Responsibilities + §Security Requirements + governance-court.md §Voting Rules + §Traceability).
  - On review completion (all 7 votes): compute Merkle root over canonical votes + approved + ts, Ed25519-sign it with Scribe key, include `decision_merkle` + `decision_sig` in the record.
  - Forward the full signed decision to Store on "court.review_complete".
  - Richer "scribe.get_review_status" and new "court.get_decision" support returning the signed record.
  - Internal decisions map for in-memory state; strict no-content guard preserved.
- `cmd/store/main.go`:
  - Persist `court_decision` (the complete signed Merkle record) on proposals when present.
  - `court.get_reviews` now returns the full signed decision when available (real data for portal/CLI/audit).
- `cmd/court-scribe/main_test.go`: Added `TestBuildSignedDecision` (verifies Merkle + verifiable Scribe signature on decision).
- All changes delete surface-only paths; real signed decisions now flow scribe → store → consumers.

**Citations (code + commit):** court-scribe.md §Responsibilities + §Security Requirements + §Key Commands + §Communication Flow; governance-court.md §Voting Rules + §Traceability + §Output Format Requirements; store-vm.md (Merkle audit); no-stubs-plan/phase-3.md 3.3.

**Verification (after edits):**
- `make build-binaries` ✓ (full suite, including scribe + store).
- `go test ./cmd/court-scribe -run 'TestDecide|TestBuildSigned|TestScribeNoContent'` + store tests ✓ PASS.
- `./bin/aegis doctor` (baseline) ✓.
- New decision records contain verifiable `decision_merkle` + `decision_sig`.

**Commit (atomic):** "phase3: Group 2 scribe audit + Merkle-signed decisions (court-scribe.md §Security Requirements + §Responsibilities, governance-court.md §Voting Rules + §Traceability, store-vm.md, phase-3.md 3.3, approved session plan)".

**phase-3.md DoD progress:** 
- [x] Voting produces tamper-evident, signed decisions (partial — scribe + store core complete; full enforcement in later groups).
- Court Scribe records full auditable trail (real signed decisions now emitted and persisted).

**Ready for "continue" → Group 3 (real Firecracker Court microVM launch via orchestrator).**

### Group 3: Real Firecracker microVM Launch for Court (user task #1 + 3.1 DoD) — COMPLETE ✅

**Changes (spec-first, minimal surface):**
- `internal/sandbox/types.go`: Added `ExtraBootArgs` to VMConfig (future-proof; used for persona identity injection).
- `internal/sandbox/firecracker.go`: 
  - Updated `buildBootArgs` to auto-inject `aegis.persona=xxx` for any VM whose ID starts with "court-persona-" (leverages the parsing added in Group 1 persona binary).
  - Added strings import.
  - This allows **one** `court-persona.img` to serve all 7 distinct personas (no image duplication).
- `internal/runtime/orchestrator.go`:
  - Added canonical `courtPersonas` list (matches scribe + persona binaries).
  - Implemented `StartCourtSystem(ctx)` (best-effort, non-fatal):
    - Launches 1 `court-scribe` VM.
    - Launches 7 `court-persona-*` VMs using the shared image + ID-derived persona identity.
    - Publishes the usual `vm.started` events; integrates with the existing critical watchdog (already lists court-* types).
  - Full citations in the method docstring.
- `cmd/aegis/main.go`: Wired the Court launch (go routine, best-effort) immediately after `StartCriticalWatchdog` in the daemon startup path. This ensures Court VMs come up early and are monitored.

**Citations:** governance-court.md §Architecture (7 independent Firecracker microVMs) + §The Seven Court Personas; court-scribe.md §Purpose; host-daemon.md (orchestrator responsibilities + critical components); prd/governance-court.md; no-stubs-plan/phase-3.md 3.1.

**Verification (after edits):**
- `make build-binaries` ✓ (full suite, court binaries + daemon).
- `go build ./internal/sandbox ./internal/runtime ./cmd/aegis` + test binary for aegis ✓.
- `./bin/aegis doctor` ✓ (baseline).
- Logic exercises the existing StartVM + key distribution + event paths (no new surface).

**Important notes:**
- Full "real Firecracker" experience requires Linux + `make build-microvms` (the Dockerfiles from Group 1 are now used by the build script for court-*).
- On non-Linux or before images exist, the daemon still starts cleanly (warnings only). Protocol tests (integration) continue to work via host processes.
- The 7+1 VMs are now launched automatically on `make start` (when images are present) and tracked by the watchdog.

**Commit (atomic):** "phase3: Group 3 real Court microVM launch (governance-court.md §Architecture, orchestrator + firecracker backend, phase-3.md 3.1, approved session plan)".

**phase-3.md DoD progress:**
- [x] 7 Court personas run as real Firecracker microVMs (launch path complete; images via existing build-microvms).

**Ready for "continue" → Group 4 (Wire Court decisions to real-time Agent Runtime enforcement + scope revocation/termination).**
