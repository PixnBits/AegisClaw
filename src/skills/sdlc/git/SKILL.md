# GitSkill v2.2 – Local Version Control for Generated Artifacts

**Status:** Reference / on-demand skill  
**Generation path:** Normally created via `coder` skill after it is bootstrapped  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, this file  
**Must enforce** every v2.1+ invariant in generated registrations and in its own behavior.

## Purpose
- Provide safe, local-only git version control for all generated skills and artifacts
- Commit individual skills immediately after registration (triggered by MemoryReflectionSkill "store" event or direct message)
- Support bulk commit of pre-git archived skills from MemoryReflectionSkill on first initialization
- Enable SDLC traceability: commit messages include skill name, model used, prompt hash/timestamp, etc.
- Allow basic operations: log, diff, branch, checkout (rollback), status — all exposed via structured messages

## Network Policy (v2.1+ Mandatory – NON-NEGOTIABLE)
```json
{
  "name": "git",
  "required_mounts": ["git-repo:rw"],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": false
}
```
Zero outbound connectivity. GitSkill **never** attempts remote operations (push, fetch, clone from URL). Local repository only.

## Required Mounts
`["git-repo:rw"]`  
- Purpose: dedicated subdirectory for the git working tree and `.git` directory  
- AegisClaw creates `./shared/git-repo` if missing and mounts it **only** to this skill  
- No access to `sources/`, `builds/`, `outputs/`, `audit/`, or any other shared subdirectory  
- Mount strategy preserves audit invariant: git history cannot tamper with audit log or source code directories

## Default Container Runtime Profile
Every service definition generated for git **MUST** inherit:
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
Exception: the `git-repo:rw` mount overrides read-only rootfs for that path only.

## Communication (Strict – hub-only)
**ALL** input/output routed exclusively through `message-hub` using structured JSON protocol.  
No direct filesystem access to host control plane, no direct TCP to aegisclaw.

**Supported message types (incoming):**
- `commit` – single skill commit  
  payload: `{ skill_name, source_code, prompt_template, binary_hash, metadata: {prompt_used_hash?, llm_model, timestamp, dependencies?} }`
- `bulk_commit` – commit multiple archived skills  
  payload: array of the above objects
- `retrieve_log`, `diff`, `status`, `checkout` – query/rollback operations
- `init` – (rare) force re-init if repo corrupted (high-risk, logged)

**Outgoing messages:**
- `commit_success` / `commit_failure` with commit hash, message, files changed
- `log_result`, `diff_result`, etc.

## Internal Behavior & Security Invariants
- Use **only** `github.com/go-git/go-git/v5` (pinned, vendored if possible) — **no** `os/exec("git")`
- Repository path from env var `GIT_REPO_DIR` (default `/data/repo`) — matches mounted volume
- On startup / first message: check if repo exists → if not, `git.Init()` plain repository (no remote)
- Commit strategy:
  - Each skill stored in structured layout, e.g.:
    - `skills/{skill_name}/main.go`
    - `skills/{skill_name}/prompt.md`
    - `skills/{skill_name}/metadata.json`
    - `skills/{skill_name}/Dockerfile` (if applicable)
  - Commit message format: `"Generated/Updated {skill_name} via {llm_model} – {short metadata summary}"`
  - Author: `"AegisClaw Coder <coder@aegisclaw.local>"`
- Never add, commit or track files outside the skill directories
- No `.gitignore` overrides that could accidentally include sensitive paths
- All operations wrapped in short `context.WithTimeout(30 * time.Second)`
- Run as non-root user inside container
- Validate incoming payloads: reject malformed JSON, missing required fields, oversized content
- Every commit success/failure → structured event sent back via hub (auditable)

## Post-Initialization Hook
After successful first bulk commit (or repo init), send structured event to hub:
```json
{
  "from": "git",
  "to": "message-hub",
  "type": "event",
  "payload": {
    "category": "git_repo_initialized",
    "repo_path": "/data/repo",
    "initial_commit_count": N,
    "timestamp": "..."
  }
}
```
Intended for SelfModSkill or future evolution components.

## Recommended Generation Prompt Excerpt (for coder skill)
"You are generating GitSkill — local-only git version control using go-git. Zero outbound networking. Mount only git-repo:rw. Commit generated skills from MemoryReflectionSkill archive. Support single & bulk commit, basic log/diff/rollback. Enforce all AegisClaw v2.1+ invariants."

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"git"|network_policy|outbound|mounts|git-repo' shared/audit/aegisclaw.log
```
shows exactly:
- that git has zero outbound
- only git-repo mount was granted
- no host network ever appeared

This SKILL.md is the binding contract for v2.2 compliance.  
Any generated code that violates networking, mount, or hub-only rules **must** be rejected during sandbox vetting by aegisclaw.
