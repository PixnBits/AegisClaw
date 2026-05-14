# 06 - Reviewing Court Decisions & Audit Log

## Overview
Users must be able to transparently review Governance Court decisions, understand why a proposal was approved or rejected, and explore the full tamper-evident audit trail of all system actions.

## User Story
As a user, I want to review Court decisions on skills or system changes and browse the complete audit history so I can maintain trust in the system’s governance and security.

## Success Criteria (Testable)
- User can list all recent Court decisions with outcome, vote breakdown, and rationale
- User can view full details of any decision (including individual persona votes and comments)
- Full audit log is queryable with filters (by proposal, skill, time range, actor)
- All audit entries are cryptographically verifiable (Merkle tree / signed)
- Court decisions are linked to the corresponding proposals and build artifacts
- Data is served from Store VM with no single point of failure

## Prerequisites for Testing
- At least one proposal that has gone through the Governance Court (from Journey #4)
- Store VM, Court Scribe, and Host Daemon running

## Step-by-Step Flow (for Implementers & Tests)

1. **View Recent Court Activity**
   - CLI: `aegis court decisions list [--limit=20]`
   - Web Portal: Navigate to “Court” → “Recent Decisions”

2. **Inspect a Specific Decision**
   - CLI: `aegis court decisions show <decision-id>`
   - Web Portal: Click on a decision card

3. **Browse Audit Log**
   - CLI: `aegis audit log [--proposal=<id>] [--skill=<name>] [--since=<date>]`
   - Web Portal: Advanced search + timeline view

4. **Verify Integrity**
   - `aegis audit verify <entry-id>` or `aegis audit verify --all`
   - System returns verification status (valid / tampered / missing)

5. **Deep Dive**
   - View linked proposal, Builder VM logs, vote transcripts, and signed artifacts

## Integration Test Requirements
- Must be able to create test proposals and simulate Court votes
- Tests should verify that rejected and approved decisions appear correctly
- Must test audit log filtering and pagination
- Must include a verification step that confirms cryptographic integrity
- Playwright tests for Web Portal: decision detail pages, search, and timeline

## Security Touchpoints
- Audit log is append-only and tamper-evident
- Users can only see their own proposals + system-wide decisions (depending on role)
- No sensitive data (secrets) ever appears in Court summaries or audit entries
- All access to audit data is logged

## CLI Commands
- `aegis court decisions list`
- `aegis court decisions show <id>`
- `aegis audit log`
- `aegis audit verify`

## Web Portal Features
- Court dashboard with vote visualizations
- Searchable timeline
- One-click “View Proposal” and “View Build Logs”

## Related Documents
- (../governance-court.md)
- (../court-scribe.md)
- (../store-vm.md)
- (../../prd/governance-court.md)
- (../../prd/security-model.md)