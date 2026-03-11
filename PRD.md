**Improved Product Requirements Document (PRD) – Version 2.1**  
**Date:** 2026-03-11  
**Status:** Bootstrap phase – hardened control plane + strict networking isolation  
**Owner:** Lead Product Owner (with Harper, Benjamin & Lucas input)

**This PRD is the single source of truth.**  
Every line of `seedclaw.go`, every `compose.yaml` edit, every generated skill, and every audit entry **must** trace back to these requirements. Security is not optional — it is the only invariant. Auditing must remain trivial: open `shared/audit/seedclaw.log` and see exactly what happened, when, and why. No surprises. Ever.

**Key Changes in v2.1 (directly addressing observed issues)**  
- Control channel switched from Unix socket to loopback-only TCP port (eliminates all permission/UID/GID issues seen in practice).  
- New mandatory **Networking Policy** section with skill-to-skill isolation, permanent ban on `network_mode: host`, default-no-internet, and explicit allow-listing.  
- “Minimal surprises” codified: every network decision is declared in metadata, enforced by seedclaw, and logged immutably.

---

### 1. Overview & Mission
SeedClaw is a minimal, local-first, self-bootstrapping, zero-trust AI agent platform. After one `go build` + `./seedclaw`, the user has a working paranoid swarm. The Trusted Computing Base is deliberately tiny: only `seedclaw.go` + the four committed core skills. Everything else is generated, sandbox-compiled, and registered with explicit least-privilege controls only.

**Tagline:** “Bootstrap your own paranoid agent swarm from markdown prompts only. No cloud. No binaries from strangers. Audit everything.”

All communication flows exclusively through `message-hub`. No skill ever touches the host filesystem, another skill’s data, or the internet unless explicitly declared and audited.

---

### 2. Core Principles (NON-NEGOTIABLE)

1. Minimal committed core, zero-code for everything else  
2. Sandbox-first, not sandbox-later  
3. Reliable self-bootstrapping loop  
4. Explicit Host-Skill Boundary (least-privilege mounts only)  
5. Single communication gateway (`message-hub` only)  
6. Immutable audit trail  
7. **Explicit & Minimal Network Surface** — Networking is never implicit. Skills are isolated from each other and the internet by default. Every network permission must be declared in registration metadata.

---

### 3. Functional Requirements

#### 3.1 Seed Binary (`src/seedclaw.go`) – MUST
- Single static Go binary.  
- On start: verify Docker, create `./shared/`, manage `compose.yaml`, start core services with selective mounts and correct networking.  
- **Control Channel (updated)**: seedclaw **MUST** listen on a loopback-only TCP port (`127.0.0.1:7124` by default; configurable via `SEEDCLAW_CONTROL_PORT` env var). It exposes a simple, secure JSON-over-TCP interface (or lightweight HTTP in future).  
  - `message-hub` is the **only** container allowed to connect (via `extra_hosts: ["host.internal:host-gateway"]` added by seedclaw).  
  - Unix sockets are permanently deprecated (permission issues observed in practice).  
- Maintain skill registry with full `network_policy` recorded.  
- Skill lifecycle (exact steps): receive generate request → forward to coder → receive bundle + metadata → sandbox compile/test → validate declared mounts + network_policy → append to `compose.yaml` with precise config → `docker compose up -d` → immutable audit entry.

#### 3.2 Message-Hub (core skill)
- Sole IPC router. Connects exclusively to seedclaw’s loopback TCP control port.  
- Enforces structured JSON with sender validation.  
- Logs every routed message to central audit trail.

#### 3.3 Core Bootstrap Skills
- Must match their `SKILL.md` contracts.  
- `llm-caller` and `ollama` receive explicit outbound allow-lists (only the domains/ports they need). All other skills default to zero outbound.

#### 3.4 Generated Skill Lifecycle
- Registration metadata **must** now include:
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
- SeedClaw **MUST reject** any registration that:  
  - omits any field above  
  - uses `network_mode` ≠ `"seedclaw-net"`  
  - sets `"outbound": "allow_list"` with empty `domains` array  
  - declares mounts not explicitly allowed by the skill's declared `required_mounts`

#### 3.5 User Interface
- `./seedclaw` (no flags) starts daemon + thin STDIN/STDOUT REPL.
- Natural-language input is forwarded via TCP to sandboxed `user-agent` skill.
- `user-agent` skill performs LLM tool-calling using live skill registry.
- All output printed back to terminal.
- Zero new host binaries, zero new ports.

---

### 4. Security & Isolation Requirements (MANIACAL FOCUS)

#### 4.1 Networking Policy (NEW – Critical Requirement)
**Design Goal:** Minimal surprises. Every network capability must be explicit, declared in metadata, enforced by seedclaw, and logged. No skill should ever have unexpected connectivity.

**Hard Rules (enforced by seedclaw on every `compose.yaml` edit):**

- **Custom Network**: All containers (core + generated) run exclusively on a dedicated Docker network `seedclaw-net` managed by seedclaw.
- **Host Network Ban**: `network_mode: host` is **permanently forbidden** for every container. Seedclaw must reject any Dockerfile, metadata, or service definition containing it. This is a non-overridable invariant.
- **Skill-to-Skill Isolation**: Skills must not communicate directly with each other. All inter-skill traffic **MUST** route exclusively through `message-hub` (enforced at generation time by coder skill prompts and at runtime by message-hub validation). Docker-level isolation is achieved via application policy + future per-skill networks (v3.0).
- **Outbound Internet Access**:
  - **Default policy: Completely blocked** (no external gateway for non-declared skills).
  - Any skill requiring outbound connectivity **must** declare an explicit allow-list in its registration metadata (see example above).
  - During registration, seedclaw logs the full policy and (MVP) relies on coder-generated code never attempting undeclared connections. Future versions will enforce at network level via dedicated egress proxy skill.
  - Only `llm-caller` and `ollama` (core) receive outbound allow-lists by default. All generated skills start with `"outbound": "none"`.
- **Control Plane Access**: Only the `message-hub` service receives the `host.internal` alias and can reach seedclaw’s TCP port. Generic skills never see this address.

This policy eliminates lateral movement, data exfiltration, and unexpected internet exposure while keeping every decision auditable in one log file.

#### 4.2 Default Container Runtime Profile (updated & hardened)
- `--network=seedclaw-net` (never `host`, never blank)
- `--rm --read-only` + tmpfs `/tmp`
- `--cap-drop=ALL --security-opt no-new-privileges`
- Strict seccomp + cgroup2 limits (512 MiB, 30 s hard timeout)
- Extra hosts & network config added **only** where declared

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
# extra_hosts: ["host.internal:host-gateway"]   # ONLY for message-hub
```

#### 4.3 Code Vetting Pipeline (mandatory)
- `go vet`, compile test, static analysis.
- New checks: reject `network_mode: host`, undeclared outbound calls, or broad network flags.
- Prompt-injection guard strengthened for network-related code.

#### 4.4 User-Facing Safety
- Natural-language requests are threat-modeled by user-agent before execution. User retains final judgement via explicit confirmation. No auto-execution of risky actions.

---

### 5. Auditing & Observability (EASY AUDITING)
- Append-only JSON lines now include full network policy:
  ```json
  {"ts":"2026-03-11T13:00:00Z","actor":"seedclaw","action":"register_skill","skill":"web-search","network_policy":{"outbound":"allow_list","domains":["*.brave.com"]},"mounts":[...],"hash":"sha256:...","previous_hash":"sha256:...","status":"success"}
  ```
- `compose.yaml` backups on every edit.
- Registry JSON versioned and includes previous hash.
- Central log shows exactly who can talk to whom and reach the internet.
- **Audit writes**: Performed **only** by seedclaw binary. message-hub sends events over TCP — **no** audit filesystem mount on message-hub.

---

### 6. Non-Functional Requirements
- Control port latency < 2 s.
- Skill startup < 5 s.
- Seedclaw binary remains tiny (< 20 MB RAM idle).
- Portability: Linux/macOS/Windows (Docker Desktop).

---

### 7. MVP Scope & Updated Success Criteria
**First-run success:**
1. `./seedclaw` → core containers running with correct TCP control channel.
2. Bootstrap prompt succeeds.
3. New skill registers with declared `network_policy`.
4. Audit log shows every network decision.
5. `./seedclaw` shows interactive prompt; user types “write hello world in Go”; coder skill runs; result appears on STDOUT.

**Security smoke tests (must pass):**
- Non-hub skills cannot reach internet or each other.
- Only `message-hub` can connect to seedclaw TCP port.
- Attempt to register skill with `network_mode: host` or undeclared outbound → rejected with audit entry.

---

### 8. Non-Goals (MVP)
- Multi-user / auth / RBAC.
- Persistent state beyond registry + audit.
- GUI.
- Cloud deployment.
- Full network-level egress enforcement (deferred to egress proxy skill).

---

### 9. Minimal Dependencies & Tech Stack
(Unchanged — Go stdlib + pinned Docker client.)

---

### 10. Phased Roadmap
- **v2.1 (current)**: TCP control + explicit networking policy + audit of every network decision.
- **v3.0**: Per-skill networks + automatic egress proxy for allow-list enforcement.
- **v4.0**: Pluggable sandbox (Docker → gVisor → Firecracker).

---

### 11. References
- ARCHITECTURE.md (to be updated with new control channel & networking layout)
- All core `SKILL.md` files (to be updated to declare `network_policy`)
- bootstrap-prompt.md (will reference v2.1)

**This PRD v2.1 replaces all previous versions.** Any generated code or architectural change that violates any MUST requirement above is invalid and must be rejected during sandbox vetting.

Approved for immediate implementation.  
— Lead Product Owner  
(2026-03-11)
