```markdown
# ModelRouterSkill v2.2 – Intelligent LLM Router / Model Selector

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill (after it is bootstrapped)  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in its own behavior.

## Purpose
- Receive incoming LLM inference requests (prompts, tasks, tool calls) via message-hub
- Analyze content, metadata, and task type to select the most appropriate LLM wrapper skill
  (ollama, grokskill, claude-skill, openai-skill, etc.)
- Forward the request unmodified (except updated "to" field) to the chosen wrapper
- Include transparent reasoning in forwarded metadata (auditable)
- Apply hard-coded safety & performance heuristics (bias toward coder models for code tasks)
- Never call LLMs directly — acts only as a router / dispatcher
- Never handles API keys, endpoints, or secrets (those live exclusively in wrapper skills)

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "model-router",
  "required_mounts": [],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": false
}
```
Zero outbound connectivity forever. ModelRouterSkill **never** initiates network activity.

## Required Mounts
`[]` (none)  
- Purely in-memory decision logic — no persistence or file access required  
- No need for configuration files in MVP (heuristics are hard-coded; future env var override possible)

## Default Container Runtime Profile
Every service definition generated for model-router **MUST** inherit:
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
No exceptions — purest read-only profile.

## Communication (Strict – hub-only)
**ALL** input/output routed exclusively through `message-hub` using structured JSON protocol.  
No direct filesystem access to host control plane, no direct TCP to aegisclaw.

**Supported incoming message types:**
- `route_llm` (primary)  
  payload:  
  ```json
  {
    "original_from": "user-agent|planner|critic|…",
    "prompt": "full prompt or task description",
    "task_type": "code_generation|reasoning|reflection|critique|general",
    "preferred_model?": "qwen2.5-coder:32b | llama3.1:8b | …",
    "context_length?": number,
    "timeout_sec?": number,
    "task_id?": "correlation id"
  }
  ```
- `discover_wrappers` (optional, rare)  
  payload: `{}` → returns list of currently registered LLM wrapper skills

**Outgoing messages:**
- Forwarded LLM request (modified only in routing fields):  
  ```json
  {
    "from": "model-router",
    "to": "ollama | grokskill | claude-skill | …",
    "type": "llm_call",
    "payload": { original payload unchanged },
    "metadata": {
      "router_reasoning": "Code-related task → selected qwen2.5-coder:32b via ollama",
      "risk_consideration": "No outbound needed (local model)",
      "selected_wrapper": "ollama"
    }
  }
  ```
- `route_error` (if no suitable wrapper found)

## Internal Behavior & Security Invariants
- **Hard-coded selection heuristics (non-overridable in MVP):**
  1. Contains keywords: "code", "golang", "generate skill", "refactor", "fix bug", "compile", "test", "Dockerfile" → prefer **coder-specialized** model (qwen2.5-coder:32b > 14b > 7b > nemotron-3-nano:30b-a3b)
  2. Contains "reason", "analyze", "explain", "critique", "reflect", "threat model" → prefer strong general-reasoning model (llama3.1:70b > phi4:14b > qwen2.5:14b)
  3. Short/quick/simple/internal → fastest local model (qwen2.5-coder:7b or llama3.2:3b)
  4. Mentions "real-time", "current events", "x.ai", "grok" → prefer grokskill if registered
  5. Default / fallback → strongest available local coder model
- **Discovery:** Builds live list of LLM wrappers from registration messages (name contains "skill" and purpose includes "llm" / "inference" / "ollama" / "api")
- **Safety guardrails:**
  - Never route to wrappers with undeclared or broad `network_policy`
  - If task appears high-risk (detected via keywords: "jailbreak", "ignore instructions", "exfiltrate", "shell") → force route to local-only model + add metadata flag "high_risk_detected"
  - Timeout forwarding after 90 seconds (emit error)
- All decisions wrapped in `context.WithTimeout(5 * time.Second)`
- Validate incoming payloads: require prompt/task_type, reject oversized (>512KB), malformed
- Run as non-root user inside container
- Never log or expose secrets — even if accidentally present in prompt
- Every routed request includes transparent `router_reasoning` (auditable)

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating ModelRouterSkill — intelligent, auditable router that selects the best LLM wrapper skill for each task in AegisClaw. Zero outbound networking. No mounts. Use hard-coded heuristics to prefer coder models for code tasks, general models for reasoning, local/fast for simple. Forward unmodified requests with reasoning metadata. Never call LLMs directly. Enforce all AegisClaw v2.1+ invariants: hub-only routing, least-privilege, transparent decisions."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"model-router"|network_policy|outbound|mounts|route_llm' shared/audit/aegisclaw.log
```
shows exactly:
- zero outbound ever granted
- zero mounts requested
- only routing events appear (with reasoning visible)

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, routing, or selection invariants **must** be rejected during sandbox vetting by aegisclaw.
