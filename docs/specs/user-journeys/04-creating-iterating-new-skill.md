# 04 - Creating / Iterating on a New Skill (SDLC Flow)

## Overview
Users (and agents) must be able to create, propose, review, and deploy new skills through a secure, governed SDLC process that enforces the Governance Court and paranoid security model.

## User Story
As a user or agent, I want to create a new skill (e.g. “GitHub PR reviewer” or “web search with summarization”) so that it goes through proper proposal, Court review, testing, and deployment before becoming available to agents.

## Success Criteria (Testable)
- User/agent can propose a new skill via natural language or structured proposal
- Proposal is stored in Store VM and routed to Governance Court
- Court personas review and vote according to current rules
- Builder VM successfully builds and tests the skill
- On approval: skill is merged into the registry and becomes immediately available
- On rejection: clear feedback is given and proposal is archived
- Full end-to-end flow completes in < 10 minutes for simple skills

## Prerequisites for Testing
- Core system running (Host Daemon, AegisHub, Store VM, Court Scribe, at least 7 Court personas)
- Builder VM backend ready
- Existing conversation with an agent that has skill-creation permissions

## Step-by-Step Flow (for Implementers & Tests)

1. **Skill Proposal**
   - User/agent says: “Add a skill that can search the web and summarize results”
   - Agent Runtime creates a structured proposal (name, description, code, permissions, tests)
   - Proposal is sent to Store VM via AegisHub

2. **Governance Court Review**
   - Court Scribe notifies all Court personas
   - Each persona reviews code, security implications, and tests
   - Court votes (Approve / Reject / Abstain)

3. **Build & Test Phase** (on tentative approval)
   - Builder VM spins up
   - Skill is built and unit/integration tests are executed
   - Artifacts are signed

4. **Final Merge**
   - On full Court approval: Store VM merges skill into registry
   - New skill becomes available to all Agent Runtime VMs
   - Audit log records the entire flow

5. **Iteration**
   - User can request changes → new proposal version is created
   - Previous versions remain in history

## Integration Test Requirements
- Must be able to submit proposals via CLI (`aegis skills propose`) and via chat
- Must simulate Court votes (both approval and rejection paths)
- Must verify Builder VM output and signed artifacts
- Must confirm new skill appears in `aegis skills list` after merge
- Use `--headless` and `--json` flags heavily
- Playwright tests for Web Portal proposal UI

## Security Touchpoints
- Skill code never runs until Court + build verification complete
- Network and secret permissions must be explicitly declared and reviewed
- Builder VM has no persistent state and egress only through Network Boundary

## CLI Commands
- `aegis skills propose`
- `aegis skills list`
- `aegis skills status <id>`
- `aegis court status`

## Related Documents
- (../builder-vm.md)
- (../store-vm.md)
- (../governance-court.md)
- (../court-scribe.md)
- (../../prd/skill-creation.md)
- (../../prd/governance-court.md)