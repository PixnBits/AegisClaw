# SelfModSkill v2.2 – Meta-Skill for Safe Self-Evolution Proposals

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill (after it is bootstrapped)  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in its own behavior.

## Purpose
- Analyze performance feedback from CriticSkill, MemoryReflectionSkill, RetryOrchestratorSkill, user escalations, or audit patterns
- Propose refinements to existing skill prompts, system templates, or architectural guidelines (as markdown text)
- Suggest creation of new skills to fill observed gaps (as structured generation requests for coder)
- Output **proposals only** — never directly modify code, prompts, SKILL.md files, registry, or compose.yaml
- Enable controlled, auditable self-evolution loop without granting write access or execution rights
- All proposals are human-reviewed (via user-agent confirmation gate) before any action

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "self-mod",
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
Zero outbound connectivity forever. SelfModSkill **never** initiates network activity of any kind.

## Required Mounts
`[]` (none)  
- No filesystem access required — all analysis is performed on incoming structured messages  
- No persistence of proposals (they are ephemeral events)  
- Future v3: optional read-only mount for long-term pattern logs (very low priority, only if critic/memory history becomes mount-shared)

## Default Container Runtime Profile
Every service definition generated for self-mod **MUST** inherit:
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
- `analyze` (primary trigger)  
  payload:  
  ```json
  {
    "source": "critic|memory-reflection|retry-orchestrator|user|audit",
    "content": "string or structured object (critique results, failure patterns, repeated escalations, …)",
    "context?": "skill_name | task_id | goal summary",
    "timestamp_range?": { "since": RFC3339, "until": RFC3339 }
  }
  ```
- `propose` (direct, rare)  
  payload: `{ target: "prompt|skill|architecture", focus: "safety|performance|efficiency|…" }`

**Outgoing messages (proposals only):**
- `proposal:prompt_refinement`  
  ```json
  {
    "type": "proposal",
    "category": "prompt_refinement",
    "target_skill": "coder|user-agent|critic|…",
    "rationale": "Detected repeated safety violations → strengthen injection guard",
    "proposed_text": "full revised system prompt as markdown string",
    "risk_level": "MEDIUM",
    "requires_confirmation": true
  }
  ```
- `proposal:new_skill`  
  ```json
  {
    "type": "proposal",
    "category": "new_skill",
    "suggested_name": "egress-proxy",
    "purpose": "Enforce network allow-lists at layer 7",
    "rationale": "Current outbound relies on registration vetting only",
    "generation_prompt_snippet": "You are generating EgressProxySkill – …",
    "risk_level": "HIGH",
    "requires_confirmation": true
  }
  ```
- `proposal:architecture` (rare) → text-only suggestion for ARCHITECTURE.md/PRD.md update

## Internal Behavior & Security Invariants
- **Analysis logic:** rule-based pattern matching + simple scoring first (no external LLM call from SelfMod in MVP)  
  - Look for repeated patterns: high retry counts, frequent critic rejections, safety violations, performance bottlenecks
  - Prioritize safety & audit invariants over convenience
  - Never propose changes that weaken `network_policy`, hub-only routing, mounts, audit chaining, or threat-model gate
- **Proposal guardrails (hard-coded):**
  - Reject any proposal that suggests: removing cap_drop, adding host network, broad mounts, self-write access, bypassing user-agent confirmation
  - All proposals marked `requires_confirmation: true` for HIGH/MEDIUM risk
  - Include clear rationale citing specific invariant violations or inefficiencies
- All operations wrapped in `context.WithTimeout(15 * time.Second)`
- Validate incoming payloads: require source/content, reject malformed or oversized (>128KB)
- Run as non-root user inside container
- Never modify any file, registry entry, or binary — proposals are pure output messages
- Every proposal → structured event sent via hub (auditable before user sees it)

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating SelfModSkill — meta-proposal engine for safe evolution of AegisClaw. Zero outbound networking. No mounts. Analyze feedback from critic/memory/retry/user, propose refined prompts or new skills as structured messages only. Never self-modify. Hard reject any proposal weakening security invariants. Enforce all AegisClaw v2.1+ invariants: hub-only, least-privilege, proposal-only."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"self-mod"|network_policy|outbound|mounts|proposal' shared/audit/aegisclaw.log
```
shows exactly:
- zero outbound ever granted
- zero mounts requested
- only proposal events appear (never execution)

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, mount, proposal-only, or evolution invariants **must** be rejected during sandbox vetting by aegisclaw.
