# 02 - Starting a New Conversation / Agent

## Overview
A user (or automated test) must be able to reliably start a new agent conversation with predictable behavior and clear observability hooks.

## User Story
As a developer or automated test, I want to start a new agent conversation so I can verify end-to-end agent runtime behavior.

## Success Criteria (Testable)
- A new agent conversation can be started in **under 5 seconds**
- The agent responds to the initial prompt
- The conversation is correctly stored and retrievable from the Memory VM
- `aegis sessions list --json` returns the new session with status `running`
- The Agent Runtime VM appears in `aegis vm list` with correct metadata
- All components shut down cleanly when the session ends

## Prerequisites for Testing
- Host Daemon, AegisHub, Memory VM, and Court Scribe must already be running
- At least one LLM available via Ollama
- System must have capacity to spin up a new Firecracker/Docker sandbox

## Step-by-Step Flow (for Implementers & Tests)

1. **Initiate Conversation**
   - **CLI**: `aegis chat --headless "Hello, who are you?"`
   - **Web Portal (E2E test)**: Use **Playwright** to:
     - Navigate to `http://localhost:8080`
     - Click "New Chat" button
     - Type message in the input field
     - Submit the message

2. **System Response**
   - Host Daemon receives request through AegisHub
   - New Agent Runtime VM is created with a unique session ID
   - VM connects to Memory VM and AegisHub

3. **Observable States**
   - Session progresses through states: `creating` → `initializing` → `ready` → `running`
   - Clear log markers:
     - `AGENT_STARTED:session=<id>`
     - `AGENT_READY:session=<id>`
     - `AGENT_FIRST_RESPONSE:session=<id>`

4. **Verification Commands**
   - `aegis sessions list --json`
   - `aegis vm list --json`
   - `aegis logs --session=<id> --tail=50`

## Integration Test Requirements
- All CLI commands must support `--json` and `--headless` flags
- Web Portal tests must use **Playwright** to drive the browser (no direct API calls for E2E journeys)
- Session IDs must be stable and easily extractable from UI or CLI
- Tests must be able to wait for specific log lines or UI updates
- Support running multiple concurrent sessions in one test suite

## Non-Goals
- Persistent sessions across daemon restarts (handled in later journeys)

## Related Documents
- (../agent-runtime.md)
- (../memory-vm.md)
- (../../prd/conversation-model.md)
- (../../prd/agent-autonomy.md)