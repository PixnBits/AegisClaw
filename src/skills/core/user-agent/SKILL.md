# User-Agent Skill v2.1.1 (2026-03-11)

Sandbox-first orchestrator for natural-language user requests.

## Network Policy (mandatory)
```json
{
  "name": "user-agent",
  "required_mounts": [],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "seedclaw-net"
  },
  "network_needed": true
}
```

## Capabilities
- Receives user prompts from seedclaw thin bridge.
- Queries skill registry via message-hub.
- Calls llm-caller with tools=skills.
- Executes ReAct loop, routing every action via message-hub.
- Formats final answer back to CLI.

## Communication (strict)
All messages via message-hub only. No direct TCP, no filesystem mounts.

## Security Invariants
- Runs with full Default Container Runtime Profile.
- Prompt injection contained inside Docker.
- Every step audited by seedclaw binary.
