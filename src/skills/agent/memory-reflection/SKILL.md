# MemoryReflectionSkill v2.2 – Episodic & Long-Term Memory + Pre-Git Archive

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill (after it is bootstrapped)  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in its own behavior and when registering future skills (if it ever gains that ability — currently read-only).

## Purpose
- Provide short-term (in-memory session) and optional long-term (persistent file) episodic memory for the swarm
- Store/retrieve key facts, user preferences, interaction summaries, reflection insights
- Act as **pre-git archive**: automatically receive and store structured records of every newly generated skill (via "store" events broadcast after registration)
- Support reflection: critique past outputs/actions, identify patterns/errors, suggest improvements
- Enable batch retrieval by category (especially `"generated_skill"`) and time range → feed directly into GitSkill for version control handoff
- All storage/retrieval is auditable and contained within least-privilege boundaries

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "memory-reflection",
  "required_mounts": ["memory:rw"],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": false
}
```
Zero outbound connectivity forever. MemoryReflectionSkill **never** attempts any network operation.

## Required Mounts
`["memory:rw"]`  
- Purpose: dedicated subdirectory for persistent JSONL storage (e.g. `./shared/memory/persistent.jsonl`)  
- AegisClaw creates `./shared/memory` if missing and mounts it **only** to this skill  
- No access to `sources/`, `builds/`, `outputs/`, `audit/`, `git-repo/`, or any other shared subdirectory  
- Mount invariant preserves audit integrity: memory contents cannot tamper with audit log, source code, or git history

## Default Container Runtime Profile
Every service definition generated for memory-reflection **MUST** inherit:
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
Exception: the `memory:rw` mount overrides read-only rootfs for that path only.

## Communication (Strict – hub-only)
**ALL** input/output routed exclusively through `message-hub` using structured JSON protocol.  
No direct filesystem access to host control plane, no direct TCP to aegisclaw.

**Supported incoming message types:**
- `store`  
  payload: `{ category: string, key?: string, value: any, timestamp: RFC3339, metadata?: {} }`  
  Special category: `"generated_skill"` — expects structured skill bundle (name, source_code, prompt_template, binary_hash, llm_model, timestamp, dependencies, …)
- `retrieve`  
  payload: `{ category?: string, key?: string, since?: RFC3339|"earliest", until?: RFC3339, limit?: number }`  
  Returns array of matching entries
- `reflect`  
  payload: `{ content: string|object, criteria?: string[] }`  
  Performs lightweight self-critique → returns `{ issues: [], suggestions: [], score: number }`
- `summarize`  
  payload: `{ category?: string, since?: RFC3339, max_length?: number }`  
  Returns condensed summary of matching entries

**Outgoing messages:**
- `store_success` / `store_failure`
- `retrieve_result` → array of entries
- `reflect_result`
- `summarize_result`

## Internal Behavior & Security Invariants
- **Short-term memory:** in-memory map (Go `map[string][]Entry`) — cleared on container restart
- **Long-term memory:** append-only JSONL file at `/data/persistent.jsonl` (matches mounted volume)  
  - Each line = one JSON object with `category`, `key`, `value`, `timestamp`, `metadata`
  - Append-only — never modify or delete existing lines
  - File created with 0600 permissions if missing
- **Generated skill archiving (pre-git):**  
  On receiving `store` with `category: "generated_skill"`:  
  - Validate structure (required fields: skill_name, source_code, prompt_template, binary_hash, llm_model, timestamp)  
  - Append full payload to JSONL  
  - Send `event` back to hub: `{ category: "skill_archived", skill_name, timestamp }`
- **Batch retrieval for GitSkill:**  
  Support query `retrieve` with `category: "generated_skill", since: "earliest"` → return all archived skills in insertion order
- All operations wrapped in `context.WithTimeout(15 * time.Second)`
- Validate incoming payloads: reject malformed JSON, oversized values (>1MB), invalid timestamps
- Run as non-root user inside container
- Never expose raw file contents to other skills — only structured, filtered responses
- Reflection uses simple rule-based or prompt-based critique (no external LLM call from this skill)

## Post-Startup / Initialization Hook
On first successful persistent write (or startup if file exists):  
send structured event to hub:
```json
{
  "from": "memory-reflection",
  "to": "message-hub",
  "type": "event",
  "payload": {
    "category": "memory_persistence_ready",
    "persistent_file": "/data/persistent.jsonl",
    "entry_count": N,
    "timestamp": "..."
  }
}
```
Intended for coordination with GitSkill, SelfModSkill, or future evolution components.

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating MemoryReflectionSkill — short-term + persistent episodic memory + pre-git archive for generated skills. Zero outbound networking. Mount only memory:rw. Store/retrieve facts, archive skill bundles on 'store' events, support batch retrieval by category/time. Enforce all AegisClaw v2.1+ invariants: hub-only routing, least-privilege, append-only persistence."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"memory-reflection"|network_policy|outbound|mounts|memory:' shared/audit/aegisclaw.log
```
shows exactly:
- zero outbound ever granted
- only `memory:rw` mount was allowed
- no host network or broad shared/ exposure

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, mount, hub-only, or persistence invariants **must** be rejected during sandbox vetting by aegisclaw.
