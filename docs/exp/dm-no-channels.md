# Experiment: DM collaboration, no channel fan-out (Arm A)

Branch `exp/dm-no-channels`.

After the project manager finishes a plan for a human goal, it does not broadcast that plan with `channel.post`, does not call `channel.add_member`, and does not wait on `channel.turn` for specialist work.

## Flow

1. `ensure.role` for each role named in the plan. The `channel` field is omitted. The orchestrator still starts the agent (the id is the role name) and does not attach it to a channel.
2. `chat.message` to each ensured agent. The payload includes the goal, the plan excerpt, and an instruction to reply to the project manager. It does not name a channel. `ensure.role` returns when the VM starts, which can be before the guest registers, so the project manager retries that send only while the hub reports the destination missing.
3. The agent handles that direct message with one LLM call and returns a hub `response` to the project manager. It does not `channel.post`. Ordinary portal chat still uses the 6-step loop.
4. The project manager synthesizes one closing answer and posts that once to the goal channel, so a harness can poll the channel for the final answer.

If the plan names no roles, that single channel post is the plan itself. That is the user-facing answer, not a specialist round.

## Court is out

This arm does not add Court or CISO members to the goal channel. The old planning block that called `channel.add_member` for `court-persona-ciso` is gone. ACLs allow `chat.message` between the project manager and role agents (`coder`, `tester`, `architect`, `ciso`, and the other on-demand role ids). They do not allow `project-manager*` → `court-persona-*`.

A plan that says "Court proposal" may still name a CISO role. That is a direct message to a role agent, not a Court seat.

Scenarios that need egress changes or a Court decision (allowlists, isolation policy, anything `court_dependent`) will not complete here. Tag and report those separately from DM-collaboration results.

## Not in this arm

No few-shot scripts, keyword oracles, or scenario fixtures were added for this path. Channel SPEAK/PASS handling is still there for turns that arrive on a channel; it is not how specialist work is assigned.
