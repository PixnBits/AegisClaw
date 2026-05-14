# Skill Creation & Extension

The system must allow agents to safely extend their own capabilities by creating new skills when needed.

## Core Capability

When a user requests a task that requires a capability the agent does not yet have, the agent may:

1. Recognize the missing capability
2. Create a formal proposal for a new skill
3. Submit that proposal to the Governance Court for review
4. If approved, implement, test, and deploy the new skill through the full SDLC

## Example

**User:** "Can you monitor my Discord server and notify me when certain keywords are mentioned?"

The agent realizes it has no Discord integration. It creates a "Discord Monitor" skill proposal, submits it to the Governance Court, and — once approved — adds the new capability to its toolbox.

## Scope

This capability is **strictly limited to creating new skills**. The agent may **not**:

- Create new agents or additional teammates
- Modify core system components
- Change the Governance Court itself
- Modify the host daemon or security policies

All new skills must pass full Court review and final user approval before activation.

## Future Consideration

Multi-agent systems and inter-agent communication will be addressed in a separate document once core single-agent capabilities are mature.
