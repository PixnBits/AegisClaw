# SeedClaw Architecture

**Version:** 1.1 (2026-03-07)  
**Status:** Bootstrap phase – minimal committed core for reliable initialization

SeedClaw is a self-hosting, local-first AI agent platform designed to be paranoid, minimal, and emergent.  
The system ships a small set of trusted starter code so users can reach a working state quickly, while preserving the core philosophy: almost everything after the initial bootstrap is AI-generated, sandboxed, and dynamically registered.

### Core Principles

1. Minimal committed core, zero-code for everything else  
   - The repository contains only:  
     - `seedclaw.go` (the host binary that runs on metal)  
     - Four bootstrap-critical skills under `/src/skills/` (each with `SKILL.md`, `*.go`, `Dockerfile`)  
     - `compose.yaml` (dynamically managed by the seed binary)  
   - All other skills, tools, and capabilities are generated, compiled, tested and registered by the system itself.

2. Sandbox-first, not sandbox-later  
   - Every skill runs inside its own Docker container.  
   - Default isolation profile for every container:  
     - Fresh ephemeral container per major invocation  
     - Read-only mounts for code/assets  
     - Ephemeral `/tmp` for writes  
     - `--network=none` by default (explicit opt-in for network access)
       - never `--network=host`
     - Dropped capabilities, no root, strict seccomp profile  
     - cgroup limits (CPU burst, memory cap at 512 MiB default)  
     - 30-second timeout kill  

3. Reliable self-bootstrapping loop  
   - The seed binary (`seedclaw`) runs outside containers and:  
     - Manages `compose.yaml` to add/remove/start/stop skill services  
     - Creates a Unix socket and mounts it into the `message-hub` container  
     - Communicates only with `message-hub` (never directly with other skills)  
   - On first run, seedclaw starts the four committed core skills via Docker Compose.  
   - All LLM calls, code generation, skill creation, etc. flow through the core skills.  
   - New skills are generated → compiled/tested in temporary sandbox → added to compose.yaml → started.

4. Trust model  
   - Only the following are trusted by default:  
     - The user-compiled `seedclaw` binary  
     - The four committed core skills (`message-hub`, `llm-caller`, `ollama`, `coder`)  
   - All LLM output and generated code/skills = untrusted/hostile by default.  
   - Before a new skill is added: static analysis (go vet, golangci-lint subset), pattern blocks (no `os/exec`, `syscall`, `unsafe`, etc.), compilation in sandbox.  
   - Binary hashing and basic self-signing checks are performed on generated binaries.

### Components

- **Seed Binary** (`src/seedclaw.go`)  
  Responsibilities:  
  - Accept user input (stdin loop minimum; WebSocket / Telegram bot optional)  
  - Create and manage a Unix socket for bi-directional communication with `message-hub`  
  - Edit `compose.yaml` to register/start/stop skills  
  - Maintain persistent skill registry (name → metadata, socket routing info)  
  - Log all significant actions immutably (stdout + optional append-only audit file)

- **Message-Hub** (`/src/skills/core/message-hub/`)  
  - Central message router (committed in repo)  
  - Listens on Unix socket created & mounted by seedclaw  
  - Routes structured JSON messages between seedclaw ↔ skills and skill ↔ skill  
  - Enforces message format, routing rules, timeouts  
  - Single point of controlled communication — no skill may talk directly to the host or to other skills except via hub

- **Core Bootstrap Skills** (all committed in repo)  
  - `message-hub` — message router (see above)  
  - `llm-caller` — thin client that speaks to local Ollama or API fallback (Claude, Grok, OpenAI, …)  
  - `ollama` — optional managed local model runner (can be disabled if user prefers external Ollama)  
  - `coder` — first generative skill; reads `SKILL.md` prompts and produces new skill code + Dockerfile + registration metadata

- **Generated Skills**  
  - Each = directory with at minimum: `SKILL.md` (prompt template), main Go file, `Dockerfile`  
  - Compiled/tested in temporary sandbox container before being added to compose.yaml  
  - Registered dynamically by seedclaw editing compose.yaml and restarting compose

### Communication Architecture

```
Host (metal)
├── seedclaw binary
│   ├── edits compose.yaml
│   └── creates & mounts Unix socket → message-hub only
│
└── Docker Compose network
    ├── message-hub (listens on mounted Unix socket)
    ├── llm-caller
    ├── ollama (optional)
    ├── coder
    └── any number of generated skills…
          ↕ (all talk exclusively to message-hub)
```

- Seedclaw ↔ message-hub: Unix socket (bi-directional, or peer pair if single-socket return path is restricted)  
- Skill ↔ skill / skill ↔ seedclaw: all messages routed through message-hub  
- No direct host-to-skill communication except the single socket mount into message-hub

### Sandbox & Isolation Evolution Path

| Level       | Isolation                  | Attack Surface                  | Overhead     | When to Use                          |
|-------------|----------------------------|----------------------------------|--------------|--------------------------------------|
| Docker      | Namespaces + cgroups + seccomp | Full host kernel                | Very low     | MVP, trusted local dev               |
| gVisor      | User-space kernel (Sentry) | Very small (Go reimpl + few host calls) | Low-medium   | Untrusted code, good perf balance    |
| Firecracker | Hardware microVM (KVM)     | Guest kernel + tiny hypervisor  | Medium       | Production, adversarial/multi-tenant |
| WASM        | TinyGo + wasmtime          | No syscalls to host             | Low          | Lightweight, no-container fallback   |

Start with rootless Docker where possible. Plan to add gVisor (`runsc`) and Firecracker runtimes later via a pluggable `sandbox-provider` abstraction.

### Threat Model (what we defend against)

- Prompt injection → generated malicious code → blocked by sandbox + static analysis  
- Container escape → prevented by seccomp, no caps, read-only mounts  
- Network exfil → default `--network=none`  
- Self-modification of seed → seedclaw binary and socket are outside containers  
- Resource exhaustion → cgroup limits + per-invocation timeouts  
- Dependency confusion / supply-chain → user compiles seed themselves  
- Rogue skill talking directly to host → only message-hub has socket access  
- Compose.yaml tampering → edits performed only by trusted seedclaw binary

### Non-Goals (for now)

- Multi-user / authentication  
- Persistent state beyond skill registry + audit log  
- GUI (chat-only interface)  
- Cloud / multi-tenant deployment  

### Roadmap Ideas

- Pluggable sandbox-provider (Docker → gVisor → Firecracker → WASM)  
- Skill revocation (compose down + registry purge + hash revocation list)  
- Audit log immutability (append-only + hash chain)  
- Multi-agent coordination / pub-sub patterns via message-hub  
- Skill signing / version pinning mechanism  

This is a living document — update as implementation progresses.
