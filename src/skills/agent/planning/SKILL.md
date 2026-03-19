# PlannerSkill v2.2 – Task Decomposition & Execution Planning

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill (after it is bootstrapped)  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in its own behavior.

## Purpose
- Decompose high-level natural-language goals or complex user requests into ordered, parallelizable, and conditionally dependent subtasks
- Produce executable plans as structured DAGs or linear sequences
- Assign subtasks to available skills (from live registry) or mark for escalation / user confirmation
- Include contingency branches (on_failure, retry, reflect, escalate)
- Support re-planning on feedback from CriticSkill, RetryOrchestratorSkill, or execution failures
- Enable safe, auditable orchestration without ever executing code itself

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "planner",
  "required_mounts": ["plans:rw?"],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": false
}
```
Zero outbound connectivity forever. PlannerSkill **never** makes network calls.

## Required Mounts
`["plans:rw"]` (optional – MVP can be `[]`)  
- Purpose: append-only storage of generated plans for audit, replay, or long-term reflection (`./shared/plans/history.jsonl`)  
- If persistence is not required initially, omit the mount entirely  
- AegisClaw creates `./shared/plans` only if requested and mounts it **exclusively** to this skill  
- No access to `sources/`, `builds/`, `outputs/`, `audit/`, `memory/`, `git-repo/`, `critiques/`, or any other shared path  
- Invariant: plans cannot tamper with code, git history, memory archive, or audit trail

## Default Container Runtime Profile
Every service definition generated for planner **MUST** inherit:
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
Exception (if persistence enabled): the `plans:rw` mount overrides read-only rootfs for that path only.

## Communication (Strict – hub-only)
**ALL** input/output routed exclusively through `message-hub` using structured JSON protocol.  
No direct filesystem access to host control plane, no direct TCP to aegisclaw.

**Supported incoming message types:**
- `plan` (primary)  
  payload:  
  ```json
  {
    "goal": "string – high-level objective",
    "context?": "string – additional background",
    "available_skills": ["array of skill names from registry"],
    "constraints?": ["array e.g. 'no outbound unless allow_list', 'low risk only', ...],
    "max_steps?": number,
    "task_id?": "correlation id"
  }
  ```
- `replan`  
  payload: `{ previous_plan_id, feedback: string|object, new_goal? }`

**Outgoing messages:**
- `plan_result`  
  ```json
  {
    "plan_id": "uuid-or-timestamp",
    "goal": "...",
    "steps": [
      {
        "step_id": 1,
        "task": "short title",
        "description": "detailed instruction",
        "assigned_to": "skill-name | user-agent | escalate",
        "depends_on": [step_ids],
        "parallel": boolean,
        "on_failure": ["retry", "reflect", "escalate", "abort"],
        "risk_level": "LOW|MEDIUM|HIGH",
        "requires_confirmation": boolean
      }
    ],
    "contingencies": [...],
    "estimated_risk": "LOW|MEDIUM|HIGH",
    "recommendation": "proceed|refine|reject"
  }
  ```

## Internal Behavior & Security Invariants
- **Planning logic:** deterministic decomposition + simple heuristics first (no external LLM call from Planner in MVP)  
  - Identify security-sensitive steps (code gen, network, file write) → mark `risk_level: HIGH`, `requires_confirmation: true`
  - Prefer routing high-risk steps through user-agent → critic loop
  - Enforce AegisClaw invariants in plan: no direct skill-to-skill comms, hub-only routing, explicit mounts/network_policy in generated skills
- **Re-planning:** on receiving failure/reflection feedback, adjust dependencies, re-assign, or insert safety gates
- **Persistence (optional via env var `PLANNER_PERSISTENCE=true`):**
  - Append full plan JSON to `/data/history.jsonl` (matches mount)
  - Append-only — never modify or delete
  - 0600 permissions
- All operations wrapped in `context.WithTimeout(30 * time.Second)`
- Validate incoming payloads: reject malformed JSON, missing goal, oversized context (>256KB)
- Run as non-root user inside container
- Never execute any assigned task — only produce plan
- If plan includes HIGH-risk steps without clear mitigation → set `recommendation: "refine"` or `"escalate"`

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating PlannerSkill — task decomposition into structured, auditable plans for AegisClaw swarm. Zero outbound networking. Optional plans:rw mount for append-only history. Produce DAG/linear plans with skill assignments, dependencies, failure contingencies, risk levels. Mark high-risk steps for user confirmation. Enforce all AegisClaw v2.1+ invariants: hub-only routing, least-privilege, security-first decomposition."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"planner"|network_policy|outbound|mounts|plans:' shared/audit/aegisclaw.log
```
shows exactly:
- zero outbound ever granted
- at most one narrow writable mount (`plans:rw`) or none
- no host network or broad shared/ exposure

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, mount, hub-only, or planning invariants **must** be rejected during sandbox vetting by aegisclaw.
