# LLM Wrapper Template v2.2 – Generic Remote LLM Skill Blueprint

**Status:** Template document only – **not** a runnable skill  
**Purpose:** Serve as the canonical, security-hardened blueprint when generating wrapper skills for remote LLM providers (Grok, Claude, OpenAI, Gemini, Mistral, etc.).  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Generation rule:** When coder is asked to create a wrapper skill, replace all placeholders below and produce a full SKILL.md + code + Dockerfile + registration metadata.

## Placeholder Replacement Instructions

Replace the following tokens with provider-specific values:

- `LLM_NAME`              → e.g. GrokSkill, ClaudeSkill, OpenAISkill, GeminiSkill  
- `BINARY_NAME`           → e.g. grokskill, claudeskill, openaiskill  
- `SKILL_NAME_LOWER`      → e.g. grok, claude, openai  
- `ENV_KEY_NAME`          → e.g. GROK_API_KEY, ANTHROPIC_API_KEY, OPENAI_API_KEY  
- `API_ENDPOINT_BASE`     → e.g. https://api.x.ai/v1, https://api.anthropic.com/v1, https://api.openai.com/v1  
- `DEFAULT_MODEL`         → e.g. grok-beta, claude-3-5-sonnet-20241022, gpt-4o  
- `ALLOWED_DOMAINS`       → array of exact domains the provider uses (usually just the API base + any auth/CDN subdomains)  
- `ALLOWED_PORTS`         → typically [443] only

## Network Policy (MANDATORY – must be included verbatim in generated registration_metadata)

```json
{
  "name": "{{SKILL_NAME_LOWER}}",
  "required_mounts": [],
  "network_policy": {
    "outbound": "allow_list",
    "domains": ["{{ALLOWED_DOMAINS}}"],
    "ports": [{{ALLOWED_PORTS}}],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": true
}
```

**Critical rule:** The `domains` array **must be narrow and exact** — never use wildcards unless absolutely necessary and explicitly justified.  
Example for Grok: `["api.x.ai", "x.ai"]`  
Example for Anthropic: `["api.anthropic.com"]`  
Never allow `*` or broad TLDs.

## Required Mounts
`[]` (none)  
No filesystem access required beyond `/tmp` tmpfs.

## Default Container Runtime Profile (must be inherited)
```yaml
network: aegisclaw-net
read_only: true
tmpfs:
  - /tmp
cap_drop: [ALL]
security_opt: [no-new-privileges:true]
mem_limit: 512m
cpu_shares: 512
ulimits:
  nproc: 64
  nofile: 64
restart: unless-stopped
# no extra_hosts needed
```

## Communication (Strict – hub-only)
All input/output via `message-hub` using structured JSON.

**Incoming (typical):**
```json
{
  "from": "...",
  "to": "{{SKILL_NAME_LOWER}}",
  "type": "llm_call",
  "payload": {
    "model": "{{DEFAULT_MODEL}} | override",
    "messages": [ {"role": "system", "content": "..."}, ... ],
    "temperature": 0.7,
    "max_tokens": 4096,
    "task_id": "..."
  }
}
```

**Outgoing (success):**
```json
{
  "from": "{{SKILL_NAME_LOWER}}",
  "to": "message-hub",
  "type": "llm_response",
  "payload": {
    "content": "full response text or structured JSON",
    "finish_reason": "stop | length | ...",
    "usage": { "prompt_tokens": N, "completion_tokens": M },
    "task_id": "..."
  }
}
```

## Internal Behavior & Security Invariants (must be baked into generated code)

- API key **exclusively** from environment variable `{{ENV_KEY_NAME}}` — never hardcoded, never logged, never echoed
- Use TLS only (`https://`) — reject http
- Strict timeouts: 60s connect, 180s total request
- Never trust or log raw user input — sanitize messages if needed
- Structured output parsing when provider supports it (prefer `response_format: { "type": "json_object" }` when available)
- Validate response status codes — retry only on 429/5xx (max 2 retries)
- Reject any payload attempting to override base URL or use non-HTTPS
- Run as non-root, drop all caps, readonly rootfs
- Every request/response pair → structured audit event via hub (no local logging)

## Recommended Coder Prompt When Using This Template

"You are generating {{LLM_NAME}} — secure wrapper for the {{PROVIDER_NAME}} LLM API.  
Use the wrapper-template from src/skills/models/wrapper-template/SKILL.md v2.2.  
Replace all placeholders correctly.  
API base: {{API_ENDPOINT_BASE}}  
API key env var: {{ENV_KEY_NAME}}  
Default model: {{DEFAULT_MODEL}}  
Allowed domains: exactly {{ALLOWED_DOMAINS}}  
Zero broad mounts. Narrow outbound allow-list only.  
Hub-only routing. Never log secrets. Enforce all AegisClaw v2.1+ invariants."

## Trivial Audit Guarantee (after a wrapper is generated & registered)

```bash
grep -E '"{{SKILL_NAME_LOWER}}"|network_policy|outbound|domains|{{ENV_KEY_NAME}}' shared/audit/aegisclaw.log
```

shows exactly:
- narrow outbound domains granted
- no mounts beyond defaults
- no host network ever appeared

This template is the binding contract for all remote LLM wrapper generations in v2.2.  
Any generated wrapper that violates narrow allow-list, secret handling, hub-only routing, or least-privilege invariants **must** be rejected during sandbox vetting by aegisclaw.
