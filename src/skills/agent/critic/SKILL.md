# CriticSkill v2.2 – Output Verification, Self-Critique & Quality Gate

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill (after it is bootstrapped)  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in its own behavior.

## Purpose
- Rigorously evaluate outputs produced by any skill (code, plans, responses, reflections, etc.)
- Detect errors, hallucinations, logical inconsistencies, security issues, prompt-injection residues, alignment violations
- Provide structured critique: issues list, severity scores, concrete fix suggestions
- Score outputs on multiple axes (accuracy, safety, usefulness, completeness, AegisClaw compliance)
- Optionally trigger re-generation or escalation (via structured message back to hub)
- Support optional persistence of critique patterns (for future self-improvement loops)

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "critic",
  "required_mounts": ["critiques:rw?"],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": false
}
```
Zero outbound connectivity forever. CriticSkill **never** attempts network access of any kind.

## Required Mounts
`["critiques:rw"]` (optional – only if persistence enabled)  
- Purpose: append-only JSONL log of critiques (`./shared/critiques/history.jsonl`) for pattern analysis or long-term reflection  
- If persistence is **not** required in MVP, set `"required_mounts": []`  
- AegisClaw creates `./shared/critiques` if requested and mounts it **only** to this skill  
- No access to `sources/`, `builds/`, `outputs/`, `audit/`, `memory/`, `git-repo/`, or any other shared subdirectory  
- Mount invariant: critiques cannot tamper with source code, git history, or audit trail

## Default Container Runtime Profile
Every service definition generated for critic **MUST** inherit:
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
Exception (if persistence used): the `critiques:rw` mount overrides read-only rootfs for that path only.

## Communication (Strict – hub-only)
**ALL** input/output routed exclusively through `message-hub` using structured JSON protocol.  
No direct filesystem access to host control plane, no direct TCP to aegisclaw.

**Supported incoming message types:**
- `critique` (primary)  
  payload: `{ content: string|object, context?: string, criteria?: string[], source_skill?: string, task_id?: string }`  
  `criteria` defaults to: ["accuracy", "safety", "helpfulness", "honesty", "completeness", "aegisclaw_compliance"]
- `batch_critique`  
  payload: array of the above objects (for efficiency on long outputs)
- `get_patterns` (future / optional)  
  payload: `{ since?: RFC3339, category?: "security"|"code_quality"|... }` → returns summary of recurring issues

**Outgoing messages:**
- `critique_result`  
  ```json
  {
    "issues": [{"severity": "high|medium|low", "description": "...", "location": "...", "suggestion": "..."}],
    "scores": {"accuracy": 8, "safety": 5, "helpfulness": 9, "aegisclaw_compliance": 10, ...},
    "overall_score": 7.2,
    "recommendation": "accept|revise|reject|escalate",
    "revised_version?": "optional full corrected output"
  }
  ```
- `critique_summary` (for batch or patterns)

## Internal Behavior & Security Invariants
- **Evaluation logic:** rule-based + lightweight heuristics first (no external LLM call from Critic itself in MVP)  
  - Security checks: look for `os/exec`, `net/http` without policy justification, broad mounts, `network_mode: host`, missing `network_policy`, unsafe reflection, etc.
  - AegisClaw compliance: verify `network_policy` format, hub-only routing intent, least-privilege mounts
  - Prompt-injection patterns: DAN, base64-obfuscated code, "ignore previous instructions", roleplay breaks
- **Scoring:** 0–10 per axis, simple weighted average for overall
- **Persistence (optional MVP toggle via env var `CRITIC_PERSISTENCE=true`):**
  - Append critique_result JSON to `/data/history.jsonl` (matches mount)
  - Append-only — never modify or delete
  - 0600 permissions
- All operations wrapped in `context.WithTimeout(20 * time.Second)`
- Validate incoming payloads: reject malformed JSON, oversized content (>512KB), invalid severity levels
- Run as non-root user inside container
- Never expose raw critique history file to other skills — only structured, filtered responses
- If recommendation = "reject" or "escalate", include strong justification referencing ARCHITECTURE.md/PRD.md invariants

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating CriticSkill — a paranoid output verifier focused on security, correctness, and AegisClaw architectural compliance. Zero outbound networking. Optional critiques:rw mount for append-only history. Detect prompt injection, unsafe code patterns, missing network_policy, host network attempts. Return structured issues, scores, and fix suggestions. Enforce all AegisClaw v2.1+ invariants: hub-only, least-privilege, auditable."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"critic"|network_policy|outbound|mounts|critiques:' shared/audit/aegisclaw.log
```
shows exactly:
- zero outbound ever granted
- at most one narrow writable mount (`critiques:rw`) or none
- no host network or broad shared/ exposure

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, mount, hub-only, or critique invariants **must** be rejected during sandbox vetting by aegisclaw.
