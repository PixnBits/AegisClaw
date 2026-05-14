# 05 - Monitoring Agent Activity & Background Tasks

## Overview
Users must have clear, real-time visibility into what their agents are doing — especially for long-running or background tasks — without needing to keep the chat window open.

## User Story
As a user, I want to monitor all my active agents and background tasks, see real-time progress, review recent actions, and intervene when necessary.

## Success Criteria (Testable)
- User can list all active conversations and background tasks with one command
- Real-time progress updates are visible in both CLI and Web Portal
- User can drill into any task to see detailed timeline, logs, and current status
- User can pause, resume, or cancel any task
- Background tasks continue running even if the user closes the CLI/Web Portal
- All activity is auditable via the Store VM audit log

## Prerequisites for Testing
- At least one active conversation or background task (from Journeys 2–4)
- System fully running (Host Daemon, AegisHub, Memory VM, Store VM)

## Step-by-Step Flow (for Implementers & Tests)

1. **View Overview**
   - CLI: `aegis tasks list` or `aegis sessions list`
   - Web Portal: Navigate to Dashboard → Active Agents / Background Tasks

2. **View Detailed Status**
   - CLI: `aegis tasks status <task-id> --watch`
   - Web Portal: Click on a task → see live timeline + streaming logs

3. **Monitor Proactive Activity**
   - User receives notifications (CLI bell, Web push, or console output) for important updates
   - Background agents continue working independently

4. **Intervene**
   - `aegis tasks pause <id>`
   - `aegis tasks resume <id>`
   - `aegis tasks cancel <id>`
   - `aegis chat --attach <session-id>` (jump back into conversation)

5. **Audit & History**
   - `aegis audit log --task=<id>`
   - Full tamper-evident history from Store VM

## Integration Test Requirements
- Tests must verify that background tasks continue after CLI/Web Portal is closed
- Must support waiting for specific status changes or log messages
- Must test pause/resume/cancel flows
- Use `--json` output for reliable parsing in tests
- Playwright tests should verify live updates in the Web Portal UI

## Security Touchpoints
- Monitoring interfaces never expose raw secrets
- Only the owner (or authorized users) can view or control a task
- All monitoring actions are logged in the Governance Court audit trail

## CLI Commands
- `aegis tasks list [--json]`
- `aegis tasks status <id> [--watch]`
- `aegis tasks pause/resume/cancel <id>`
- `aegis audit log`

## Web Portal Features
- Dashboard with live cards for each agent/task
- Real-time log streaming
- Notification center for proactive agent messages

## Related Documents
- (../memory-vm.md)
- (../store-vm.md)
- (../agent-runtime.md)
- (../../prd/agent-autonomy.md)