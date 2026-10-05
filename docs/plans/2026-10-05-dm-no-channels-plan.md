# DM / no-channels (Arm A)

Session-settled. Do not reopen these.

## Decisions

- After a human goal is planned, do not `channel.post` the plan, do not `channel.add_member`, and do not use `channel.turn` to assign specialist work.
- `ensure.role` omits `channel`. An empty channel still spawns the agent (`id` is the role). Do not invent a channel id.
- Deliver work with existing `chat.message` (no new `agent.task`). Payload carries the goal, the plan excerpt, and an instruction to reply to the project manager.
- The agent answers that direct message with one LLM call and a hub `response` to the project manager. It does not `channel.post`. Portal `chat.message` stays on the 6-step loop.
- When replies are in, the project manager synthesizes one closing answer and `channel.post`s that once on the goal channel. If the plan names no roles, that single post is the plan itself (user-facing answer, no specialist round).
- ACLs: `project-manager*` ↔ role agents for `chat.message`, both directions. No `project-manager*` → `court-persona-*`.
- Remove the planning `channel.add_member` of `court-persona-ciso`. Court stays out.

## Out

- Few-shots, keyword oracles, and scenario fixtures in product code.
- Pushing, updating `main`, force-push.

## Tests

- Planning with named roles: ensure without `channel`, one `chat.message` per role, no `channel.add_member`, exactly one closing `channel.post`.
- Planning with no roles, or a failed plan: one post, no direct messages.
- Agent: a project-manager `chat.message` replies to that source and does not `channel.post`.
- ACL load: role-agent `chat.message` allowed; Court `chat.message` denied.
