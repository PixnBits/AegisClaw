# Agent Initiative & Autonomy

The agent begins cautious and earns greater autonomy through explicit review.

## Autonomy Levels

- **Level 0 – Passive:** Only responds when spoken to (default for new agents)
- **Level 1 – Proactive:** May initiate conversation with updates, discoveries, or clarifying questions
- **Level 2 – Independent:** May execute skills and run background tasks without asking permission first

## Trust Graduation Process

Advancing from Level 1 to Level 2 requires a formal **Governance Court review**.

The Court is given:
- All Court Scribe summaries of past conversations
- User corrections and feedback
- The agent's current `soul.md` and system prompt
- Any model-specific tuning files
- Current per-agent SETTINGS.yaml and usage metrics summary (new)

The Court votes on whether the agent has demonstrated reliable judgment and should be granted independent action rights. The user has final veto power.

## Court Scribe

To keep reviews efficient, a lightweight **Court Scribe** (running in its own isolated microVM) observes conversations in real time and produces short, structured summaries after each significant interaction. These summaries are stored in the tamper-evident audit log and used during promotion reviews instead of raw conversation history.

## Per-Agent Customization & Observability

Users control each agent individually via `~/.aegis/agents/<name>/` (SOUL.md, SETTINGS.yaml for model params / tool scopes / extra instructions, skills). The system tracks detailed LLM usage metrics (tokens in/out, model, time-bucketed aggregates) and exposes them on the web portal's individual agents page. This gives users direct visibility into behavior and "compute spend" while preserving strong isolation. Metrics and setting changes feed into trust decisions and Court reviews.

## Philosophy

The agent must behave like a trusted colleague who has been given a mission — not like a tool that only moves when commanded. Trust is earned, never assumed. Clear customization boundaries and honest usage metrics are core to that trust.

## Related Documents

- **[../architecture.md](../architecture.md)** — High-level system architecture
- **[runtime-architecture.md](./runtime-architecture.md)** — Runtime requirements
- **[glossary.md](./glossary.md)** — Key term definitions
- **Component Specs** — [../../specs/](../../specs/) (where applicable, especially `agent-customization.md`)
- Web portal individual agents page requirements (in `specs/agent-customization.md` and `specs/web-portal/implementation-gaps-and-priorities.md`)