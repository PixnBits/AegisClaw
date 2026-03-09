# SeedClaw Architecture

**Version:** 1.2 (2026-03-07)  
**Status:** Bootstrap phase – minimal committed core + explicit shared boundary

SeedClaw is a self-hosting, local-first AI agent platform designed to be paranoid, minimal, and emergent.  
The system ships a small trusted core so users reach a working state quickly. Everything beyond the core is AI-generated, sandboxed, and dynamically registered.

## Core Principles

1. **Minimal committed core, zero-code for everything else**  
   The repository contains only:  
   - `seedclaw.go` (the host binary that runs on metal)  
   - Four bootstrap-critical skills under `/src/skills/` (each with `SKILL.md`, `*.go`, `Dockerfile`)  
   - `compose.yaml` (dynamically managed by the seed binary)  
   All other skills, tools, and capabilities are generated, compiled, tested and registered by the system itself.

2. **Sandbox-first, not sandbox-later**  
   Every skill runs inside its own Docker container.  
   Default isolation profile for every container:  
   - Fresh ephemeral container per major invocation  
   - Read-only mounts where possible  
   - Ephemeral `/tmp` for writes  
   - `--network=none` by default (explicit opt-in for network access)  
   - Dropped capabilities, no root, strict seccomp profile  
   - cgroup limits (CPU burst, memory cap at 512 MiB default)  
   - 30-second timeout kill  

3. **Reliable self-bootstrapping loop**  
   - The seed binary (`seedclaw`) runs outside containers and:  
     - Manages `compose.yaml` to add/remove/start/stop skill services  
     - Creates purpose-specific Unix sockets and mounts only what each skill needs  
     - Communicates only with `message-hub` (never directly with other skills)  
   - On first run, seedclaw starts the four committed core skills via Docker Compose.  
   - All LLM calls, code generation, skill creation, etc. flow through the core skills.  
   - New skills are generated → compiled/tested in temporary sandbox → added to compose.yaml → started.

4. **Explicit Host-Skill Boundary (least-privilege mounts)**  
   All data exchange between the host and containers happens through a single top-level `./shared/` directory (sibling to `compose.yaml`).  
   **No skill container ever receives a blanket mount of the entire shared directory.**  
   Instead, each skill receives only the specific purpose-named subdirectories it actually needs, with the narrowest read/write permissions required.  
   This design dramatically improves auditability, makes the attack surface explicit, and follows strict least-privilege principles.

## Shared Directory Structure

Located at `./shared/` (created and managed automatically by the seed binary).  
Only purpose-driven subdirectories are ever mounted:

- `sockets/control/` — Host↔skill control uses a small, explicit IPC proxy.
  - Seedclaw will run a minimal, local-only control proxy (TCP on loopback or a short-lived protected unix socket) and accept connections from `message-hub` over the Docker network.
  - Enables stronger transport protections (mTLS, short-lived tokens, loopback-only binding).
- `sources/` — Read-only templates and AI-generated source code before compilation  
- `builds/` — Temporary compilation workspaces and output binaries  
- `outputs/` — Skill-produced artifacts (logs, data files, etc.)  
- `logs/` — Centralized debug and operational logs  
- `audit/` — Immutable append-only audit trail (future hash-chained files)

- **Mount strategy (security key point):**
- Prefer *no* host bind-mount for the control channel. Instead:
  - `seedclaw` runs the IPC proxy bound to loopback and/or listens on an internal unix socket not shared by generic skills.
  - `message-hub` connects to the proxy over the Docker network (or to a localhost-forwarded port) and does not receive a host `shared/` volume for control access.
  - `message-hub` still receives only the precise `shared/` subdirectories it needs for other purposes (e.g., `sources`/`builds`), but not the control socket path.
- `coder` skill gets `./shared/sources:ro` + `./shared/builds:rw`  
- Future generated skills declare exactly which subdirectories they require in their registration metadata.  
- The seed binary adds only those precise volume lines to `compose.yaml`.  
- Most skills receive zero mounts unless they explicitly need them.

This prevents any skill from seeing sockets or files belonging to other skills or the host at large.

## Components

- **Seed Binary** (`src/seedclaw.go`)  
  Responsibilities:  
  - Accept user input (stdin loop minimum; WebSocket / Telegram bot optional)  
  - Create and manage `./shared/` subdirectories and purpose-specific sockets  
  - Edit `compose.yaml` to register/start/stop skills with selective mounts only  
  - Maintain persistent skill registry (name → metadata, required mounts, socket routing info)  
  - Log all significant actions immutably (stdout + optional append-only audit file)

- **Message-Hub** (`/src/skills/core/message-hub/`)  
  - Central message router (committed in repo)  
  - Connects exclusively to `./shared/sockets/control/seedclaw.sock` (mounted as `/run/sockets/control/seedclaw.sock`)  
  - Routes structured JSON messages between seedclaw ↔ skills and skill ↔ skill  
  - Only skill allowed to communicate directly with the host

- **Core Bootstrap Skills** (all committed in repo)  
  - `message-hub` — message router (see above)  
  - `llm-caller` — thin client that speaks to local Ollama or API fallback (Claude, Grok, OpenAI, …)  
  - `ollama` — optional managed local model runner  
  - `coder` — first generative skill; reads `SKILL.md` prompts and produces new skill code + Dockerfile + registration metadata (including required shared subdirs)

- **Generated Skills**  
  - Each = directory with at minimum: `SKILL.md` (prompt template), main Go file, `Dockerfile`  
  - Compiled/tested in temporary sandbox container before being added to compose.yaml  
  - During registration the skill declares its required shared subdirectories; seedclaw adds only those mounts

## Communication Architecture

```
Host (metal)
├── seedclaw binary
│   ├── creates/manages ./shared/
│   ├── creates ./shared/sockets/control/seedclaw.sock
│   └── edits compose.yaml (with selective per-skill mounts only)
│
└── Docker Compose network
    ├── message-hub (mounted only /sockets/control)
    ├── llm-caller
    ├── ollama (optional)
    ├── coder (mounted only sources + builds)
    └── any generated skill…
          ↕ (all talk exclusively to message-hub)
```

- Seedclaw ↔ message-hub: single purpose-specific Unix socket in `./shared/sockets/control/`  
- Skill ↔ skill / skill ↔ seedclaw: all messages routed through message-hub  
- No skill can see another skill’s socket or files

## Sandbox & Isolation Evolution Path

| Level       | Isolation                  | Attack Surface                  | Overhead     | When to Use                          |
|-------------|----------------------------|----------------------------------|--------------|--------------------------------------|
| Docker      | Namespaces + cgroups + seccomp | Full host kernel                | Very low     | MVP, trusted local dev               |
| gVisor      | User-space kernel (Sentry) | Very small (Go reimpl + few host calls) | Low-medium   | Untrusted code, good perf balance    |
| Firecracker | Hardware microVM (KVM)     | Guest kernel + tiny hypervisor  | Medium       | Production, adversarial/multi-tenant |
| WASM        | TinyGo + wasmtime          | No syscalls to host             | Low          | Lightweight, no-container fallback   |

Start with rootless Docker where possible. Future upgrades via pluggable `sandbox-provider` abstraction.

## Threat Model (what we defend against)

- Prompt injection → generated malicious code → blocked by sandbox + static analysis  
- Container escape → prevented by seccomp, no caps, read-only mounts  
- Network exfil → default `--network=none`  
- Self-modification of seed → seedclaw binary and shared/ are outside containers  
- Resource exhaustion → cgroup limits + per-invocation timeouts  
- Dependency confusion / supply-chain → user compiles seed themselves  
- Rogue skill seeing other skills’ data → prevented by purpose-specific mounts only  
- Broad host filesystem exposure → eliminated by never mounting entire shared/ or unrelated paths  
- Compose.yaml tampering → edits performed only by trusted seedclaw binary

## Non-Goals (for now)

- Multi-user / authentication  
- Persistent state beyond skill registry + audit log  
- GUI (chat-only interface)  
- Cloud / multi-tenant deployment  

## Roadmap Ideas

- Skill-declared mount requirements parsed automatically during generation  
- Read-only vs read-write enforcement enforced by seedclaw  
- Audit directory with hash chaining and immutability  
- tmpfs/ephemeral mounts for even stricter isolation  
- Multi-agent coordination / pub-sub patterns via message-hub  
- Pluggable sandbox-provider (Docker → gVisor → Firecracker → WASM)  

This is a living document — update as implementation progresses.
