# 08 - Multi-agent / Team Workflows

## Overview
Users should be able to orchestrate multiple specialized agents that collaborate on a shared goal, with clear handoffs, shared memory, and coordinated task execution — **without** triggering the full skill-creation / Governance Court flow.

## User Story
As a user, I want to spawn multiple specialized agents (e.g. Researcher + Analyst + Writer + Critic) that work together on a complex project, share context safely, and coordinate their efforts.

## Success Criteria (Testable)
- User can start a multi-agent team session with named roles
- Agents can delegate subtasks to each other
- Shared context is available via Memory VM without leaking permissions
- User sees a unified team view with individual agent contributions
- Agents can hand off work cleanly with state transfer
- User can intervene, reassign roles, or disband the team at any time

## Example Use Case
“Analyze the pros and cons of adopting Zig for our next systems project. Produce a detailed report with code examples and risk assessment.”

Roles spawned:
- **Researcher** — gathers information and benchmarks
- **Analyst** — evaluates technical tradeoffs and risks
- **Coder** — creates example code snippets
- **Critic** — reviews and stress-tests the output

## Step-by-Step Flow

1. **Start Multi-Agent Session**
   - CLI: `aegis team new "Analyze adopting Zig..." --roles=researcher,analyst,coder,critic`
   - Web Portal: “New Team Project” → define goal + assign roles

2. **Team Initialization**
   - Multiple Agent Runtime VMs are created
   - Each receives a role-specific system prompt + shared team context in Memory VM

3. **Collaborative Execution**
   - Researcher shares findings → Analyst processes them → Coder produces examples → Critic reviews
   - Agents proactively message the team or user when needed

4. **User Oversight**
   - Unified team chat / dashboard
   - `@researcher` or `@team` mentions for directed communication

5. **Completion**
   - Final consolidated output delivered
   - Team session can be archived or converted to a single-agent follow-up

## Integration Test Requirements
- Must support spawning 3–6 concurrent agents in one team
- Verify safe, permission-checked context sharing via Memory VM
- Test handoff scenarios and role-based delegation
- Confirm no unintended Court proposals are triggered by normal team collaboration
- Playwright tests for team dashboard and cross-agent messaging

## Security Touchpoints
- Each agent operates with its own isolated skill permissions
- Inter-agent communication is routed through AegisHub and audited
- Shared Memory VM enforces access controls per agent

## CLI Commands
- `aegis team new <goal> [--roles=...]`
- `aegis team list`
- `aegis team status <team-id>`
- `aegis team message <team-id> @role "message"`

## Related Documents
- (../agent-runtime.md)
- (../memory-vm.md)
- (../../prd/agent-autonomy.md)
- (../../prd/conversation-model.md)