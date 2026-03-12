# Coder Skill v2.1.3

**Status in v2.2:** Reference implementation only.  
Not started automatically.  
First on-demand skill most users generate after bootstrap.  
Must still enforce full v2.1+ network_policy rules when producing new skills.

Generates new skills compliant with SeedClaw ARCHITECTURE.md v2.1 and PRD.md v2.1. Uses nemotron-3-nano:30b-q4_K_M (NVIDIA) or equivalent strong coding / agentic model.

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

## Recommended Model for RTX 3060-class hardware (12 GB VRAM)
`nemotron-3-nano:30b-q4_K_M` (NVIDIA). Runs via Ollama layer offloading; 16 GB+ VRAM recommended for full speed.

## Required Mounts
["sources:ro", "builds:rw"] only.

## Default Container Runtime Profile
read_only: true, tmpfs /tmp, cap_drop ALL, mem_limit 512m, seedclaw-net only.

## Communication
**ALL** LLM calls and skill registration route exclusively through message-hub. No direct access.
