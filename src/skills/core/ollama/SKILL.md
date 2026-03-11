# Ollama Skill v2.1

Manages local Ollama instance for model serving and pulls. Containerized with strict isolation.

## Capabilities
- Serve models locally (http://ollama:11434)
- Pull models on demand
- List available models

## Network Policy (v2.1 Mandatory)
```json
{
  "name": "ollama",
  "required_mounts": ["ollama/models:rw"],
  "network_policy": {
    "outbound": "allow_list",
    "domains": ["registry.ollama.ai", "ollama.com"],
    "ports": [443],
    "network_mode": "seedclaw-net"
  },
  "network_needed": true
}
```
Outbound ONLY for model registry pulls. Blocked otherwise.

## Required Mounts
["ollama/models:rw"] (for model storage; managed by seedclaw). No other shared/ access.

## Default Container Runtime Profile
Compliant: read_only: true (except models tmpfs), cap_drop: [ALL], mem_limit: 512m, network: seedclaw-net.

## Communication
Exclusively via message-hub. Other skills call ollama only through hub. Internal serving on Docker network only.

## Message Format
**Incoming:**
```json
{
  "from": "sender",
  "to": "ollama",
  "content": {
    "action": "pull",
    "model": "qwen2.5-coder:32b"
  }
}
```

## Security & Auditing Invariants
- Pull actions audited with domains in seedclaw.log.
- No inter-skill direct access.
- Model storage isolated.
- Trivial grep auditing for any network activity.
- Enforces v2.1 policy in all interactions.
