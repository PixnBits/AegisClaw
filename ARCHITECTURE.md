# SeedClaw Architecture

**Version:** 2.1 (2026-03-11)  
**Status:** Hardened bootstrap phase – TCP control plane + mandatory explicit networking policy  
**Alignment:** 100% with PRD.md v2.1 (single source of truth for every line of code, every compose.yaml edit, every skill registration)

SeedClaw is a self-hosting, local-first, zero-trust AI agent platform that is paranoid by design.  
The Trusted Computing Base is deliberately microscopic: only `seedclaw.go` + four committed core skills. Everything else is generated, sandbox-compiled in a temporary container, statically vetted, and registered with **explicit least-privilege mounts + mandatory `network_policy`**.  

No surprises. Ever.  
Every network decision, every mount, every registration is logged immutably in `./shared/audit/seedclaw.log` (append-only JSON lines with SHA-256 hashes). Auditing any skill’s connectivity or filesystem access is a single `grep`.

## Core Principles (NON-NEGOTIABLE – enforced by seedclaw)

1. Minimal committed core, zero-code for everything else  
2. Sandbox-first, not sandbox-later  
3. Reliable self-bootstrapping loop  
4. Explicit Host-Skill Boundary (least-privilege mounts ONLY)  
5. Single communication gateway (`message-hub` only)  
6. Immutable audit trail (every network decision included)  
7. **Explicit & Minimal Network Surface** – networking is never implicit. Default = zero outbound, zero inter-skill direct comms, zero host-network exposure.

## Shared Directory Structure ( `./shared/` – created & managed exclusively by seedclaw )

Only purpose-driven subdirectories are ever mounted. **Control channel is now pure TCP** (no filesystem socket mount).

- `sources/` — read-only AI-generated source code & templates  
- `builds/` — temporary compilation workspaces + binaries  
- `outputs/` — skill-produced artifacts  
- `ollama/models/` — persistent model storage for ollama skill (rw for ollama only)  
- `logs/` — centralized operational logs  
- `audit/` — immutable append-only trail (JSON + hash-chained)

**Mount strategy (security invariant):**  
- No skill ever receives the entire `shared/` directory.  
- `coder` receives only `sources:ro` + `builds:rw`.  
- `ollama` receives `ollama/models:rw` — no other skill receives this mount.
- Future skills declare exact required subdirs in registration metadata; seedclaw adds **only** those lines to `compose.yaml`.  
- `message-hub` receives **no** control-related mounts (TCP only).

## Components

### Seed Binary (`src/seedclaw.go` – the only binary that runs on metal)
**Single static Go binary** (<20 MiB idle). Responsibilities (all actions audited):

- Verify Docker, create `./shared/`, manage dedicated `seedclaw-net` Docker network.  
- **Control Channel**: listen **exclusively** on `127.0.0.1:7124` (configurable via `SEEDCLAW_CONTROL_PORT`). JSON-over-TCP (mTLS-capable in future).  
- Maintain persistent skill registry (`shared/registry.json`) with full `network_policy`, mounts, and previous hash.  
- Skill lifecycle (exact sequence):  
  1. Receive generate request → route to `coder`.  
  2. Receive bundle + metadata.  
  3. Sandbox compile/test (`go vet`, static analysis, reject `network_mode: host`).  
  4. **Validate** declared `required_mounts` + `network_policy`.  
  5. Append to `compose.yaml` with **precise** network & mount config only.  
  6. `docker compose up -d` → immutable audit entry.  
- **Rejection rules** (logged + rejected): `network_mode: host`, missing `network_policy`, undeclared outbound, broad mounts.

### Message-Hub (core skill – sole IPC router)
- Connects **exclusively** to seedclaw’s TCP port via `extra_hosts: ["host.internal:host-gateway"]`.  
- Enforces structured JSON, sender validation, and routes **everything** (skill↔skill, skill↔seedclaw).  
- Logs every message to audit trail.

### Core Bootstrap Skills (committed – updated SKILL.md forthcoming)
- `message-hub` – TCP control only.  
- `llm-caller` – explicit outbound allow-list (OpenAI, Anthropic, Grok, etc.).  
- `ollama` – explicit outbound for model pulls.  
- `coder` – generates skills **including** mandatory `network_policy`.

### Generated Skills (all future skills)
Must ship:
- `SKILL.md` (prompt template)  
- Go main + `Dockerfile`  
- Registration metadata **containing** `network_policy` (enforced by coder prompt + seedclaw vetting)

## Skill Registration Metadata Schema (mandatory – enforced at registration)

```json
{
  "name": "skill-name",
  "required_mounts": ["sources:ro", "outputs:rw"],
  "network_policy": {
    "outbound": "none" | "allow_list",
    "domains": ["api.example.com", "*.example.org"],
    "ports": [443],
    "network_mode": "seedclaw-net"          // MUST — never "host", "bridge", "none", etc.
  },
  "network_needed": false,
  "hash": "sha256:................................................",
  "timestamp": "2026-03-11T13:45:22Z",
  "previous_hash": "sha256:................................................"
}
```

SeedClaw **MUST reject** any registration that:  
- omits any field above  
- uses `network_mode` ≠ `"seedclaw-net"`  
- sets `"outbound": "allow_list"` with empty `domains` array  
- declares mounts not explicitly allowed by the skill's declared `required_mounts`

## Networking Architecture & Policy (NEW – Critical Section)

All containers run exclusively on the dedicated `seedclaw-net` (created by seedclaw).

**Hard Rules (enforced by seedclaw on every compose.yaml edit – non-overridable):**

1. **Custom Network Only** – every service: `network: seedclaw-net`.  
2. **Host Network Ban** – `network_mode: host` permanently forbidden. Any occurrence (Dockerfile, metadata, compose) → registration rejected + audit entry.  
3. **Skill-to-Skill Isolation** – no direct TCP/UDP between skills. All communication **MUST** route through `message-hub` (enforced by coder generation prompts + runtime validation in message-hub).  
4. **Outbound Internet Access**  
   - Default: completely blocked (no gateway).  
   - Only skills that explicitly declare an allow-list in `network_policy` may have outbound.  
   - Core skills (`llm-caller`, `ollama`) receive narrow allow-lists by default. All generated skills start with `"outbound": "none"`.  
   - Future v3.0: egress proxy skill will enforce at network level.  
5. **Control Plane Access** – ONLY `message-hub` receives `host.internal` alias and can reach `127.0.0.1:7124`. Generic skills never see this address.

**Default Container Runtime Profile (applied to every service):**
```yaml
network: seedclaw-net
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
extra_hosts:
  - "host.internal:host-gateway"   # only for message-hub
```

## Communication Architecture

```
Host (metal)
├── seedclaw binary
│   ├── listens 127.0.0.1:7124 (loopback-only TCP)
│   ├── thin STDIN→TCP bridge (user REPL)
│   ├── manages seedclaw-net + compose.yaml
│   └── immutable audit log (writes only)
│
└── Docker network: seedclaw-net
    ├── message-hub (sole router, TCP client to host.internal:7124)
    ├── user-agent (new core skill – agent loop, tool calling)
    ├── llm-caller
    ├── ollama
    ├── coder
    └── generated skills…
          ↕ (ALL communication via message-hub only)
```

**User interaction (new):**  
`./seedclaw` starts daemon + interactive REPL on STDIN/STDOUT. Every line is forwarded as JSON to `user-agent` skill. No LLM code lives in the binary.

## Sandbox & Isolation Evolution Path

| Level       | Isolation                  | Attack Surface                  | Overhead     | When to Use                          |
|-------------|----------------------------|----------------------------------|--------------|--------------------------------------|
| Docker      | Namespaces + cgroups + seccomp | Full host kernel                | Very low     | MVP (current)                        |
| gVisor      | User-space kernel          | Very small                      | Low-medium   | Untrusted code                       |
| Firecracker | MicroVM (KVM)              | Tiny hypervisor                 | Medium       | Production                           |
| WASM        | wasmtime + TinyGo          | No syscalls                     | Low          | Lightweight fallback                 |

## Threat Model & Defenses (maniacal focus)

- Prompt injection → malicious code → blocked by sandbox vetting + `network_policy` enforcement.  
- Container escape → prevented by read-only, cap-drop, seccomp, cgroup limits.  
- Network exfil / lateral movement → prevented by default no-outbound + message-hub-only routing.  
- Rogue skill reaching internet → rejected at registration unless explicitly allowed and audited.  
- Compose.yaml tampering → performed only by seedclaw binary.  
- Broad host exposure → eliminated by selective mounts + TCP control.  
- Audit tampering → append-only + SHA-256 chaining (future).

**Trivial auditing guarantee:** `grep -E '"network_policy|outbound|domains|network_mode"' shared/audit/seedclaw.log` shows exactly what connectivity exists on the entire swarm.

## Auditing & Observability

**Audit trail implementation (v2.1 hardened):**  
All entries are written **exclusively by the seedclaw host binary** to `./shared/audit/seedclaw.log` (append-only JSON Lines).  
`message-hub` sends structured audit events over the TCP control channel — it **never** receives a filesystem mount for audit logging.  
Every entry includes `previous_hash` for SHA-256 chaining (tamper-evident).

Every significant action is a JSON line:
```json
{"ts":"2026-03-11T13:00:00Z","actor":"seedclaw","action":"register_skill","skill":"web-search","network_policy":{...},"mounts":[...],"hash":"sha256:...","previous_hash":"sha256:...","status":"success"}
```

`compose.yaml` is backed up before every edit. Registry is versioned.

## Non-Goals (MVP)

- Multi-user / auth / RBAC  
- Persistent state beyond registry + audit  
- GUI (chat-only)  
- Cloud / multi-tenant  

## Roadmap (aligned with PRD)

- **v2.1 (current)**: TCP control + explicit networking policy + audit of every network decision.  
- **v3.0**: Per-skill networks + automatic egress proxy.  
- **v4.0**: Pluggable sandbox provider (Docker → gVisor → Firecracker).  

## References (all prompts must reference this document)

- PRD.md v2.1  
- All core `SKILL.md` files (to be updated to enforce `network_policy`)  
- bootstrap-prompt.md (will reference v2.1)

**This ARCHITECTURE.md v2.1 replaces all previous versions.**  
Any generated code, skill, or architectural change that violates any requirement above is invalid and must be rejected during sandbox vetting.

— Lead Architect (2026-03-11)
