# Coder Skill v2.1

Generates new skills compliant with SeedClaw ARCHITECTURE.md v2.1 and PRD.md v2.1. Uses qwen2.5-coder or equivalent.

## Capabilities
- Reads SKILL.md templates
- Produces full skill (Go code, Dockerfile, SKILL.md, metadata)
- **MANDATORY**: Includes valid network_policy in every generated registration metadata

## Network Policy (v2.1 Mandatory)
```json
{
  "name": "coder",
  "required_mounts": ["sources:ro", "builds:rw"],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "seedclaw-net"
  },
  "network_needed": false
}
```
- Zero internet outbound by default.
- When generating skills: **MUST** embed correct network_policy (allow_list only when justified, never host network, default none).

## Required Mounts
["sources:ro", "builds:rw"] only.

## Default Container Runtime Profile
read_only: true, tmpfs /tmp, cap_drop ALL, mem_limit 512m, seedclaw-net only.

## Communication
**ALL** LLM calls and skill registration route exclusively through message-hub. No direct access.

## Message Format
**Incoming:**
```json
{
  "from": "sender",
  "to": "coder",
  "content": {
    "action": "generate_skill",
    "skill_name": "...",
    "prompt": "..."
  }
}
```
**Outgoing:** Bundle with full metadata including network_policy.

## Security & Auditing Invariants
- Every generated skill MUST declare network_policy; seedclaw rejects violations.
- Coder prompt (in generation) references this SKILL.md v2.1 as contract.
- All actions (generation + compile) audited immutably.
- Trivial auditing: grep network_policy shows exactly what connectivity every skill has.
- Enforces zero-surprises, least-privilege, hub-only, no-host-network invariants in ALL output.
- Dual-purpose: human doc + generation contract.
