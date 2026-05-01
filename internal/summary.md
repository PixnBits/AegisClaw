# `internal/` — Directory Summary

## Overview

The `internal/` directory contains all AegisClaw core packages. Together they implement the full daemon: Firecracker VM lifecycle, Governance Court review pipeline, skill builder, encrypted vault, LLM proxy, audit log, event bus, web dashboard, and more. No package is directly importable outside this module.

Every package is designed around the **paranoid-by-design** invariant from `docs/architecture.md`: component boundaries are security boundaries; untrusted input always runs inside Firecracker microVMs; all inter-VM communication is ACL-gated through AegisHub.

## Package Table

| Package | One-line description |
|---|---|
| [`api`](api/summary.md) | Unix socket API layer between the CLI and the daemon; `Client`/`Server` with HTTP/JSON envelope over `/run/aegisclaw.sock` |
| [`audit`](audit/summary.md) | Two subsystems: Ed25519-signed append-only Merkle chain log (tamper-evident) and per-session JSONL conversation log |
| [`builder`](builder/summary.md) | Automated skill build pipeline: LLM code generation → compile/test/lint → security gates → git commit → signed artifact |
| [`builder/securitygate`](builder/securitygate/summary.md) | Four mandatory security gates (SAST, SCA, secrets scanning, policy-as-code); stdlib-only for maximum auditability |
| [`composition`](composition/summary.md) | Versioned deployment manifest store with automatic rollback on health failures; resolves PRD deviation D10 |
| [`config`](config/summary.md) | Single `Config` struct (authoritative daemon config schema); loaded from `~/.config/aegisclaw/config.yaml` via Viper |
| [`court`](court/summary.md) | Governance Court: multi-round, multi-persona LLM review in sandboxed microVMs; weighted consensus; full audit trail |
| [`dashboard`](dashboard/summary.md) | Local web portal (Phase 4): pure Go `html/template` pages, SSE live updates, in-browser approvals, served at `:7878` |
| [`eval`](eval/summary.md) | Synthetic acceptance-test harness (Phase 5): three agentic scenarios (background research, OSS issue→PR, recurring summary) |
| [`eventbus`](eventbus/summary.md) | Host-level async backbone: persistent Timer service (cron), Signal subscriptions, Human Approval queue; all Merkle-logged |
| [`gateway`](gateway/summary.md) | Multi-channel message gateway: routes inbound messages from webhooks, Discord, Telegram, and other adapters to the daemon |
| [`ipc`](ipc/summary.md) | AegisHub IPC mesh: `MessageHub`, `Router`, ACL enforcement, `IdentityRegistry`; sole vsock routing plane for all inter-VM traffic |
| [`kernel`](kernel/summary.md) | Singleton cryptographic core: Ed25519 keypair, Merkle audit log, vsock `ControlPlane`; every auditable operation flows through `Kernel.SignAndLog` |
| [`llm`](llm/summary.md) | LLM infrastructure: Ollama client, structured output enforcement, persona-based model routing, proxy, cross-model verification |
| [`lookup`](lookup/summary.md) | Semantic tool-discovery store: chromem-go vector DB, FNV-32 embeddings (384-dim), Gemma 4 `<\|tool\|>` block formatting |
| [`memory`](memory/summary.md) | Secure long-term agent memory: age-encrypted append-only JSONL vault, tiered TTLs, PII scrubber (7 regex rules), UUID-keyed entries |
| [`proposal`](proposal/summary.md) | Governance proposal data model and persistence: FSM (draft→approved/rejected), SHA-256 Merkle hash chain, git-backed store |
| [`provision`](provision/summary.md) | Idempotent Firecracker asset provisioning: downloads `vmlinux`, builds Alpine rootfs, installs `guest-agent` binary |
| [`registry`](registry/summary.md) | Read-only HTTP client for ClawHub skill registry (`registry.clawhub.io`); every import requires Governance Court approval |
| [`runtime`](runtime/summary.md) | Parent package for the task execution subsystem; contains `runtime/exec` |
| [`runtime/exec`](runtime/exec/summary.md) | `TaskExecutor` interface, `ReActRunner` FSM, Firecracker production executor, in-process test executor (build-tag gated), Ollama cassette recorder |
| [`sandbox`](sandbox/summary.md) | Full Firecracker microVM lifecycle: `SandboxSpec`/`SandboxInfo` types, `SandboxManager` interface, nftables network policy, snapshot/cleanup |
| [`sbom`](sbom/summary.md) | CycloneDX 1.4 JSON SBOM generation for skills: dependency detection from `go.mod`, SHA-256 aggregate hash, `skill.sbom` tool |
| [`sessions`](sessions/summary.md) | In-memory ephemeral session registry: conversation history (capped at 200 messages), lifecycle states, 100-session capacity with LRU eviction |
| [`testutil`](testutil/summary.md) | VCR-style Ollama HTTP recorder for integration tests; cassette replay without live Ollama; stored in `testdata/cassettes/` |
| [`tui`](tui/summary.md) | Terminal UI (bubbletea/Elm architecture): chat, Court dashboard, audit explorer, system status, shared style system |
| [`vault`](vault/summary.md) | Age-encrypted secret storage and delivery: per-secret `.age` files, HKDF-derived vault key, `SecretProxy` for vsock injection with memory zeroing |
| [`wizard`](wizard/summary.md) | Interactive 8-step terminal wizard (charmbracelet/huh) for authoring governance proposals; automatic risk scoring into 4 tiers |
| [`worker`](worker/summary.md) | Ephemeral Worker sub-agent management: 4 roles (Researcher, Coder, Summarizer, Custom), persistent JSON lifecycle store, crash recovery |
| [`workspace`](workspace/summary.md) | Loads optional Markdown customisation files (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`) from `~/.aegisclaw/workspace/`; 16 KiB per-file cap |

## Architectural Patterns

### Security Isolation
Every package that touches untrusted input operates inside Firecracker microVMs. Host-side packages (`api`, `kernel`, `audit`, `config`) have minimal TCB and no LLM calls.

### Communication
All inter-VM traffic routes through `internal/ipc` (AegisHub). Direct VM-to-VM paths do not exist. The daemon's `internal/api` exposes a Unix socket for the unprivileged CLI.

### Cryptographic Audit Trail
`internal/kernel` + `internal/audit` provide the Ed25519-signed append-only Merkle chain that records every significant action. Every package that performs a security-relevant operation calls `Kernel.SignAndLog`.

### Skill Lifecycle
`internal/proposal` → `internal/court` → `internal/builder` (+ `securitygate`) → `internal/composition` → `internal/sandbox`. This pipeline is the core SDLC enforcement mechanism.

### Data Security
`internal/vault` (secrets), `internal/memory` (agent memory), and `internal/audit` (logs) all use age/Ed25519 encryption. Keys are derived from the daemon's keypair in `internal/kernel`.
