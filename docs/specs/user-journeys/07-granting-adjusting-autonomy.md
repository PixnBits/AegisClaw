# 07 - Granting / Adjusting Agent Autonomy

## Overview
Users must be able to safely grant, adjust, and revoke autonomy to agents using simple natural language while the system enforces clear security boundaries and Court oversight for risky actions.

## User Story
As a user, I want to easily give my agent more independence (e.g. “allow background research for 30 minutes”) without needing to learn technical scope names.

## Success Criteria (Testable)
- Users can grant/revoke autonomy using natural language in chat or simple CLI commands
- System maps user intent to precise internal scopes
- Changes take effect immediately
- High-risk actions still require explicit approval or Court review
- All changes are auditable

## Autonomy Scopes (Defined by Skills)
Every skill declares its required scopes in `skill.yaml` (reviewed by Court):
- `background-execution` (with max duration)
- `network-access` (allowed domains or “none”)
- `code-execution`
- `file-write`
- `skill-creation`
- `external-api` (with rate limits)
- etc.

## Easy User Experience

**Preferred method – Natural Language:**
- “Give yourself permission to research on the web for the next hour”
- “Let this agent run code without asking me”
- “Switch to research mode for 30 minutes”

**Presets (Easy Templates):**
- **Research Mode** — network + summarization allowed, time-limited
- **Code Mode** — code execution + file write, requires user approval for commits
- **Safe Mode** — minimal permissions, always ask

## Step-by-Step Flow

1. **View Current Autonomy**
   - `aegis autonomy show <session-id>`
   - Or ask in chat: “What permissions do you currently have?”

2. **Grant Autonomy**
   - Natural language in chat, or
   - CLI: `aegis autonomy grant <session-id> --preset=research --duration=30m`
   - Per-agent permission grants/requests (distinct from autonomy scopes) are also reviewable and actionable in the Portal trace view (Agents → trace).

3. **Agent Confirmation**
   - Agent replies: “Understood. I now have web research permission for 30 minutes.”

4. **Revoke**
   - “Stop and ask me before every action” or `aegis autonomy reset <session-id>`

## Integration Test Requirements
- Tests must simulate natural language autonomy requests
- Verify mapping from user intent → internal scopes
- Test preset application and expiration
- Verify Court is triggered for high-risk scopes

## Security Touchpoints
- All scope grants are logged
- Natural language requests are parsed and shown to user for confirmation before activation
- Court approval required for dangerous scopes (e.g. `skill-creation`, broad `network-access`)

## Related Documents
- (../agent-runtime.md)
- (../../prd/agent-autonomy.md)
- (../governance-court.md)
- (../network-boundary.md)