# AegisClaw — Repository Summary

## What It Is

**AegisClaw** is a paranoid-by-design, local AI agent platform where every untrusted component — including LLM inference, skill execution, agent runtime, and governance review — runs inside an isolated [Firecracker](https://firecracker-microvm.github.io/) microVM. The host OS surface is minimal: only the daemon and the CLI are host-native. KVM is a hard requirement; there is no fallback execution mode.

The design is governed by five principles from `docs/PRD.md`:
1. **Firecracker isolation** for all untrusted workloads (LLM, skills, agents, review)
2. **Governance Court** of five LLM personas must approve every proposed skill before it is built or deployed
3. **Append-only Merkle audit log** (Ed25519-signed) records every action; tampering is detectable
4. **Encrypted at rest** — secrets (age + HKDF), memory (age JSONL), and logs — with PII scrubbing
5. **Local-only LLM** (Ollama) — no telemetry, no cloud calls

## Repository Layout

```
AegisClaw/
├── cmd/                  # Four binaries (one host-side, three in-VM)
├── config/               # Runtime YAML configuration (personas, templates)
├── deploy/               # Firecracker VM overlay config and network setup
├── docs/                 # Full specification suite (PRD, architecture, plans, tutorials)
├── internal/             # All library packages (~28 packages)
├── scripts/              # Build scripts for rootfs images and live testing
├── testdata/             # Golden traces and Ollama cassettes for offline tests
├── go.mod / go.sum       # Go module: github.com/PixnBits/AegisClaw, Go 1.25+
├── Makefile              # Top-level build targets
├── AGENTS.md             # Workspace customisation loaded by `internal/workspace`
└── CONTRIBUTING.md       # Test taxonomy, build-tag rules, security policy
```

## Binaries (`cmd/`)

| Binary | Runs on | Role |
|---|---|---|
| `aegisclaw` | Host | Main CLI + daemon (`start`/`stop`/`skill`/`session`/`audit`/`court`/`proposal`/`vault`/`memory` commands) |
| `guest-agent` | Agent microVM | ReAct loop, tool dispatch, session management, memory access |
| `aegishub` | AegisHub microVM | Sole vsock IPC router; ACL enforcement; all inter-VM message brokering |
| `aegisportal` | Portal microVM | Local web dashboard (`:7878`); Go `html/template` + SSE live updates |

## Internal Package Organisation

See [`internal/summary.md`](internal/summary.md) for the full package table. Packages group by concern:

| Concern | Packages |
|---|---|
| Cryptographic core & IPC | `kernel`, `ipc`, `audit` |
| VM lifecycle | `sandbox`, `provision`, `runtime/exec` |
| Skill lifecycle | `proposal`, `court`, `builder`, `builder/securitygate`, `composition`, `registry`, `sbom` |
| Agent loop | `llm`, `lookup`, `memory`, `sessions`, `worker`, `workspace` |
| Host API | `api`, `config`, `vault`, `eventbus`, `gateway` |
| UI | `tui`, `dashboard`, `wizard` |
| Testing | `testutil`, `eval` |

## Key Architectural Patterns

### All-In-Microvm Rule
Every component that processes untrusted content (LLM output, skill code, external messages) runs in a Firecracker microVM with its own rootfs. Host packages have minimal TCB.

### AegisHub as Sole Router
`internal/ipc` implements AegisHub — the only message-passing fabric. VMs communicate via vsock through AegisHub; direct VM-to-VM paths are prohibited.

### Skill Lifecycle Pipeline
```
Proposal (wizard/CLI) → Governance Court (×5 LLM personas) → Builder VM
  → Security Gates (SAST, SCA, secrets, policy) → Signed artifact
  → Composition manifest → Deployment
```

### Governance Court
Five personas (`CISO`, `SeniorCoder`, `SecurityArchitect`, `Tester`, `UserAdvocate`) vote with weights summing to 1.0. CISO uses ensemble mode; others use fallback. Multi-round deliberation in isolated microVMs. Full Merkle-logged audit trail.

### Cryptographic Foundations
- **`internal/kernel`**: Ed25519 keypair loaded at daemon start; all `SignAndLog` calls flow here
- **`internal/audit`**: append-only Merkle chain (SHA-256 linking + Ed25519 per-entry signature)
- **`internal/vault`**: per-secret age-encrypted files; vault key = HKDF-SHA256 of Ed25519 seed
- **`internal/memory`**: age-encrypted append-only JSONL; 7-rule PII scrubber; tiered TTLs

## Key Dependencies

| Dependency | Role |
|---|---|
| `filippo.io/age v1.3.1` | Symmetric file encryption (vault, memory) |
| `github.com/firecracker-microvm/firecracker-go-sdk v1.0.0` | Firecracker VM management |
| `github.com/charmbracelet/bubbletea` | Terminal UI (Elm architecture) |
| `github.com/philippgille/chromem-go v0.7.0` | In-process vector DB for skill lookup |
| `github.com/spf13/cobra v1.8.1` | CLI command tree |
| `github.com/spf13/viper v1.21.0` | Config file loading |
| `go.uber.org/zap v1.27.1` | Structured logging |
| `golang.org/x/crypto v0.45.0` | HKDF, additional crypto primitives |
| `github.com/go-git/go-git/v5 v5.17.0` | Git operations in builder pipeline |

## Further Reading

- [`docs/architecture.md`](docs/architecture.md) — authoritative component model
- [`docs/PRD.md`](docs/PRD.md) — product requirements and security principles
- [`docs/first-skill-tutorial.md`](docs/first-skill-tutorial.md) — end-to-end skill authoring guide
- [`docs/threat-model.md`](docs/threat-model.md) — trust boundaries and attack mitigations
- [`internal/summary.md`](internal/summary.md) — all internal packages with one-line descriptions
- [`cmd/summary.md`](cmd/summary.md) — CLI/binary entry points
