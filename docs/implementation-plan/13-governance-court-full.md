# 13 - Full Governance Court (7 Personas + Court Scribe)

**Goal**: Implement the complete 7-persona Governance Court + Court Scribe as defined in `docs/specs/governance-court.md` and `docs/prd/governance-court.md`.

## Current State
Partial court engine exists, but the full 7-persona system + Court Scribe integration is incomplete.

## Tasks

1. **Define the 7 personas**
   - Create dedicated persona prompts and decision models (Security Auditor, Ethics Reviewer, etc.)
2. **Implement Court Scribe**
   - Scribe observes conversations and generates structured summaries for the Court
   - Per `docs/specs/court-scribe.md`
3. **Court deliberation engine**
   - Multi-persona parallel review with structured voting
   - Consensus and veto rules
4. **Integration**
   - Wire into proposal flow and builder trigger
   - Full audit logging of every court decision
5. **Tests**
   - End-to-end court review with all 7 personas
   - Scribe summary quality tests

## Acceptance Criteria
- All 7 personas participate in reviews
- Court Scribe produces high-quality structured summaries
- Full alignment with `docs/specs/governance-court.md`

**Dependencies**: Court engine + event system
**Estimated effort**: 3–4 days

**Owner**: TBD
**Status**: Ready to start