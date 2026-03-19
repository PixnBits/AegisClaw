# RetryOrchestratorSkill v2.2 – Failure Detection & Recovery Orchestration

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill (after it is bootstrapped)  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in its own behavior.

## Purpose
- Passively monitor the swarm message bus (via hub broadcasts) for error/failure/timeout/rejection events
- Track failure counts per task/plan/step (in-memory correlation via task_id / plan_id)
- Decide and emit structured recovery actions:
  - retry same step/skill (with exponential backoff hint)
  - refine prompt / parameters (forward to LLMSelector or user-agent)
  - swap skill (e.g. fall back to weaker/faster model)
  - reflect (send to CriticSkill or MemoryReflectionSkill)
  - escalate to user (via user-agent confirmation gate)
  - abort (with final explanation)
- Limit total retries per task (hard cap 3–5, configurable via env)
- Prevent retry storms and infinite loops
- Never execute tasks itself — only coordinate recovery

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "retry-orchestrator",
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
Zero outbound connectivity forever. RetryOrchestratorSkill **never** initiates network activity.

## Required Mounts
`[]` (none in MVP)  
- No persistent state required initially — all tracking is in-memory (map keyed by task_id/plan_id)  
- Container restart → lost retry state → conservative fallback to escalate/abort  
- Future v3: optional `retries:rw` for append-only failure log (very low priority)

## Default Container Runtime Profile
Every service definition generated for retry-orchestrator **MUST** inherit:
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
No exceptions — purest read-only profile possible.

## Communication (Strict – hub-only)
**ALL** input/output routed exclusively through `message-hub` using structured JSON protocol.  
No direct filesystem access to host control plane, no direct TCP to aegisclaw.

**Passive listening (primary mode):**
- Subscribes to all messages of type `error`, `failure`, `timeout`, `rejection`, `critique_result` (where recommendation = "revise"|"reject")
- Filters for messages containing `task_id`, `plan_id`, `step_id`, or correlation identifiers

**Supported active incoming messages (rare / optional):**
- `configure`  
  payload: `{ max_retries: number, backoff_base_sec: number, default_action: "retry|reflect|escalate" }`
- `status`  
  payload: `{ task_id?: string }` → returns current retry count & last action

**Outgoing messages (recovery directives):**
- `retry`  
  ```json
  {
    "type": "retry",
    "task_id": "...",
    "step_id": 3,
    "attempt": 2,
    "backoff_sec": 4,
    "reason": "transient LLM timeout",
    "target": "original_skill_name"
  }
  ```
- `refine` → sends to LLMSelector / user-agent with suggested prompt changes
- `swap_skill` → suggests alternative skill name
- `reflect` → routes to CriticSkill or MemoryReflectionSkill
- `escalate` → routes back to user-agent for confirmation / abort
- `abort` → final failure with explanation

## Internal Behavior & Security Invariants
- **State:** in-memory map[task_id → {attempts: number, last_error: string, history: []}]  
  - Cleared on container restart (conservative by design)
  - Hard cap: max_retries (default 5) → force escalate/abort
- **Decision logic:** simple state-machine rules (no external LLM calls in MVP)  
  - Transient errors (timeout, rate-limit) → retry + backoff (1s → 2s → 4s → …)
  - Persistent errors (validation failure, safety violation) → reflect → escalate
  - Critic low score (<4) → refine or swap
  - Security violation detected → immediate abort + escalate
- All decisions wrapped in `context.WithTimeout(10 * time.Second)`
- Validate incoming error payloads: require `task_id` or correlation id, reject malformed
- Run as non-root user inside container
- Never modify or re-execute any task — only emit directives
- Every emitted recovery action → structured event logged via hub (auditable)

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating RetryOrchestratorSkill — passive failure watcher and recovery coordinator for AegisClaw. Zero outbound networking. No mounts. Listen to hub for error/failure messages, track retry counts per task_id, emit retry/refine/swap/reflect/escalate/abort directives. Hard retry cap. Enforce all AegisClaw v2.1+ invariants: hub-only, least-privilege, no side-effects beyond messages."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"retry-orchestrator"|network_policy|outbound|mounts' shared/audit/aegisclaw.log
```
shows exactly:
- zero outbound ever granted
- zero mounts requested
- purest possible container profile

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, mount, or orchestration invariants **must** be rejected during sandbox vetting by aegisclaw.
