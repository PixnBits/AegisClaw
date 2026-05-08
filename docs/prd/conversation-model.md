# Conversation Model

The system shall support **bidirectional, asynchronous communication** between user and agent, rather than strict request-response turns.

## Why This Matters

Most AI agents today operate in a rigid back-and-forth pattern. This feels more like interrogating a tool than collaborating with a colleague. We want the agent to feel like a proactive team member.

## Key Behaviors

- The user may send messages, clarifications, new tasks, or corrections at any time
- The agent may initiate contact when it has relevant updates, discovers new information, or needs clarification
- The agent may run long-lived tasks in the background and report back when complete or when significant events occur
- The interface must clearly distinguish between user-initiated and agent-initiated messages
- The system must gracefully handle interleaved streams of activity without losing context

## Example: Background Research with Proactive Updates

**User:** "Research the best e-ink tablets for taking handwritten notes. Focus on battery life and writing feel."

While the user is busy elsewhere, the agent discovers:
- A new 2026 model was just released with significantly better battery life
- One of the top models has a serious flaw with writing latency

The agent proactively sends two messages:

> Hey, I found a brand new 2026 model that looks really strong for your use case.

> Quick heads-up — the Remarkable 3 has worse writing latency than the marketing suggests. Might want to avoid it.

## Success Criteria

A user can send clarifying messages, walk away for hours, and return to find the agent has thoughtfully kept them updated — all within the same coherent conversation.

