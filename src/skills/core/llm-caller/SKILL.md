# LLM Caller Skill v2.1

Thin, secure client for LLM inference. Supports local Ollama and approved remote providers. All calls routed through message-hub.

## Capabilities
- Calls to approved LLM endpoints
- Handles auth via env vars (injected by seedclaw)
- Structured prompt/response handling

## Network Policy (v2.1 Mandatory)
```json
{
  "name": "llm-caller",
  "required_mounts": [],
  "network_policy": {
    "outbound": "allow_list",
    "domains": ["api.openai.com", "api.anthropic.com", "grok.x.ai", "ollama.ai", "registry.ollama.ai"],
    "ports": [443],
    "network_mode": "seedclaw-net"
  },
  "network_needed": true
}
```
Narrow allow-list ONLY for approved LLM providers. No other outbound.

## Required Mounts
[] (none). No filesystem access beyond defaults.

## Default Container Runtime Profile
read_only: true, tmpfs: [/tmp], cap_drop: [ALL], security_opt: [no-new-privileges:true], mem_limit: 512m, network: seedclaw-net (NEVER host).

## Communication
**ALL** requests/responses route exclusively through message-hub. No direct HTTP from other skills or host sockets.

## Message Format
**Incoming:**
```json
{
  "from": "sender",
  "to": "llm-caller",
  "content": {
    "action": "call",
    "provider": "openai",
    "model": "...",
    "prompt": "..."
  },
  "metadata": {"sender_validated": true}
}
```

## Security & Auditing Invariants
- Every outbound call logged with full network_policy in seedclaw.log.
- Only uses declared domains/ports.
- Trivial audit via grep on outbound and domains.
- Coder skill must copy this exact policy style when generating similar skills.
- Immutable, least-privilege, hub-only routing enforced.
