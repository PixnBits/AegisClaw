# Agent Customization Specification

## Overview
Users can customize agent behavior and provide skills through the `~/.aegis/agents/` directory structure. This supports both shared configuration and per-agent customization.

## Directory Structure

```
~/.aegis/agents/
├── shared/
│   ├── TOOLS.md
│   └── skills/
├── default/
│   ├── SOUL.md
│   ├── AGENTS.md
│   └── skills/
├── researcher/
│   ├── SOUL.md
│   ├── AGENTS.md
│   └── skills/
└── analyst/
    ├── SOUL.md
    ├── AGENTS.md
    └── skills/
```

### Skills Format (agentskills.io style)
Each skill lives in its own folder inside `skills/` (e.g. `web-research/`) with at least `SKILL.md` containing YAML frontmatter.

## Loading Rules
- `shared/` files apply to all agents
- Per-agent files override shared ones
- Skills in an agent's `skills/` folder are only available to that agent

## Traceability
**Driven by:**
- Multi-agent team workflows (Journey #8)
- Strong user customization needs