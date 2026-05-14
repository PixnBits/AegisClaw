# 03 - Collaborative Task Execution (with Proactive Updates)

## Overview
The user and agent should be able to work together on complex, multi-step tasks. The agent must be able to proactively update the user with progress, ask clarifying questions, and continue working even when the user is not actively responding.

## User Story
As a user, I want to give an agent a complex goal (e.g. “research and draft a comparison of Rust vs Go for our new service”) and have it collaborate with me in real time — providing updates, seeking approval when needed, and continuing work in the background.

## Success Criteria (Testable)
- Agent can execute multi-step tasks that span > 5 minutes
- User receives proactive status updates at logical breakpoints
- Agent pauses and asks for clarification/approval on high-impact decisions
- User can respond to a proactive message and the agent resumes correctly
- Conversation history and task state survive agent VM restarts
- User can see real-time progress in both CLI and Web Portal

## Prerequisites for Testing
- At least one active conversation (from Journey #2)
- Relevant skills/tools available (e.g. web search, code execution)
- Memory VM and AegisHub running

## Step-by-Step Flow (for Implementers & Tests)

1. **User assigns a complex task**
   - CLI: `aegis chat --headless "Research X and draft a report"`
   - Web Portal: User types the goal in an existing chat

2. **Agent begins execution**
   - Agent Runtime enters the 6-step loop (Observe → Think → Plan → Act → Execute → Judge)
   - Agent creates structured task plan in Memory VM

3. **Proactive Updates**
   - Agent sends status messages like:
     - “I’ve completed initial research. Here are the top 3 findings…”
     - “I need your approval before calling the GitHub API with these credentials”
   - Updates are pushed asynchronously via AegisHub

4. **User Interaction**
   - User replies in the same conversation
   - Agent receives the message and continues from the current plan step

5. **Background Continuation**
   - If user becomes idle, agent continues working (within configured autonomy limits)
   - Progress is saved in Memory VM

## Integration Test Requirements
- Tests must be able to simulate user responses with delays (e.g. “wait 30 seconds then reply”)
- Must verify proactive messages appear in the UI/CLI without user input
- Must be able to inject approvals/rejections and confirm agent behavior
- Use Playwright for Web Portal tests to verify real-time message streaming
- Check that task state is correctly persisted in Memory VM after agent restart

## Security Touchpoints
- Agent cannot use high-privilege skills without explicit user + Court approval
- All outbound actions (network, code execution) go through Network Boundary and skill declarations
- Proactive updates never include unredacted secrets

## CLI / Web Commands
- `aegis chat` (interactive)
- `aegis tasks list` — show active background tasks
- `aegis tasks status <id>`
- Web Portal: Real-time streaming + notification badges

## Related Documents
- (../agent-runtime.md)
- (../../prd/agent-autonomy.md)
- (../../prd/conversation-model.md)
- (../memory-vm.md)
- (../network-boundary.md)