# 09 - Adding a Discord Monitor Skill (Full SDLC Example)

## Overview
This journey demonstrates the **complete end-to-end SDLC** for adding a new skill (“Discord Monitor”) — strictly following the governance process defined in `docs/prd/sdlc-governance.md`. No code change is allowed without formal Governance Court approval.

## User Story
As a user, I want to add a new skill that monitors a Discord server for specific keywords or events and notifies me (or my agents) so that I can react in real time — and have the entire process go through proper proposal, Court review, implementation, testing, sign-off, and deployment.

## Success Criteria (Testable)
- Full SDLC completes end-to-end with zero shortcuts
- Proposal is created and stored in Store VM
- Governance Court (all 7 personas) reviews and votes
- Builder VM builds and tests the skill
- Final Court sign-off occurs before merge
- Skill becomes available to all agents after deployment
- Audit log contains the complete tamper-evident history

## Full SDLC Process (Compliant with sdlc-governance.md)

1. **Proposal**
   - User (or agent) says in chat: “Add a Discord Monitor skill that watches a server for keywords and sends summaries”
   - Agent Runtime creates a structured proposal (skill name, description, required scopes, code skeleton, tests)
   - Proposal is submitted to Store VM via AegisHub

2. **Court Review**
   - Court Scribe notifies all 7 Court personas
   - Each persona reviews code, security implications, permissions (Discord token handling, network access), and tests
   - Court votes (Approve / Reject / Abstain)

3. **Implementation**
   - On tentative approval: Builder VM spins up
   - Full skill code is written/implemented inside the isolated microVM
   - Skill declares exact scopes (`network-access:discord.com`, `background-execution`, etc.)

4. **Testing & Validation**
   - Automated unit + integration tests run inside Builder VM
   - Manual validation steps (if required by Court)
   - Security scan and permission verification

5. **Court Sign-off**
   - Court performs final review of the implemented code, test results, and build artifacts
   - Final vote required before deployment

6. **Deployment**
   - Only after full Court approval: Store VM merges the skill into the official registry
   - Skill is immediately available to all Agent Runtime VMs
   - Announcement sent to user and audit log updated

## Integration Test Requirements
- Must be able to trigger the entire flow via natural language in chat
- Must simulate Court votes (approval + rejection paths)
- Must verify Builder VM output, test results, and signed artifacts
- Must confirm skill appears in `aegis skills list` only after final merge
- Full audit log verification at the end

## Security Touchpoints
- Skill never runs until Court sign-off
- Discord token/credentials handled only inside Network Boundary VM
- All steps are logged in the tamper-evident audit trail

## CLI Commands
- `aegis skills propose`
- `aegis court decisions list`
- `aegis skills list` (shows new skill only after deployment)

## Related Documents
- (../builder-vm.md)
- (../store-vm.md)
- (../governance-court.md)
- (../court-scribe.md)
- (../network-boundary.md)
- (../../prd/sdlc-governance.md)
- (../../prd/skill-creation.md)
- (04-creating-iterating-new-skill.md)