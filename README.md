# AegisClaw

**Paranoid-by-design, self-evolving local agent platform**

Secure, local-first AI agent runtime with:
- Firecracker microVM isolation for every skill, reviewer, and builder
- Governance Court: multi-persona AI review for every code change
- Mandatory security gates (SAST, SCA, secrets scanning, policy-as-code)
- Append-only Merkle-tree audit log with Ed25519 signatures
- Currently Ollama-only on Linux
- Terminal UI (TUI) for interactive ReAct-style chat & agent control

## Getting Started

### Prerequisites

| Requirement | Notes |
|---|---|
| **Linux** (x86_64) | Firecracker requires KVM; falls back to direct mode without `/dev/kvm` |
| **Go 1.26+** | Build from source |
| **Ollama** | LLM inference — install from [ollama.com](https://ollama.com) |

### 1. Build

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw
go build -o aegisclaw ./cmd/aegisclaw
go build -o guest-agent ./cmd/guest-agent
```

### 2. Initialize

One-time setup — creates directory structure, keypair, and audit log:

```bash
./aegisclaw init
```

### 3. Start the Daemon

```bash
sudo ./aegisclaw start &
```

### 4. Open Chat

```bash
./aegisclaw chat
```

Type a message or `/help` for available commands.

### 5. Create Your First Skill

In chat, describe what you want:

```
please add a skill that says hello to the user with a message appropriate
for the time of day ("good morning", "good evening", etc.) respecting DST,
in en-US
```

The agent creates a proposal, submits it for Court review, builds it in a
sandboxed pipeline, and activates the skill — all automatically.

Or use the CLI directly:

```bash
./aegisclaw skill add "time-of-day greeter" \
  --non-interactive \
  --name time-of-day-greeter \
  --tool "greet:Returns a locale-aware DST-respecting greeting"
```

> **📖 Full walkthrough:** See **[docs/first-skill-tutorial.md](docs/first-skill-tutorial.md)** for a
> thorough step-by-step guide covering the entire lifecycle — proposal, Court
> review, builder pipeline, security gates, activation, and invocation.

---

## CLI Commands

```
aegisclaw init          One-time setup
aegisclaw start         Start the coordinator daemon
aegisclaw stop          Gracefully stop the daemon
aegisclaw status        Show system status and health
aegisclaw chat          Interactive ReAct chat (primary interface)
aegisclaw skill         Manage skills (add, list, revoke, info)
aegisclaw audit         Query the append-only audit log
aegisclaw secrets       Manage secrets (add, list, rotate)
aegisclaw self          Self-improvement and system management
aegisclaw version       Show version and build information
```

Global flags: `--json`, `--verbose/-v`, `--dry-run`, `--force`

---

## Security Architecture

Every skill runs in its own **Firecracker microVM** with:
- Read-only rootfs
- No network access (unless explicitly declared and approved)
- `cap-drop ALL` — no Linux capabilities
- Secrets injected via proxy at runtime (never in code)

Every code change goes through:
1. **Governance Court** — 5 AI personas review in isolated microVMs
2. **Builder pipeline** — code generated in a sandboxed microVM
3. **Security gates** — SAST, SCA, secrets scanning, policy-as-code (mandatory, no bypass)
4. **Versioned deployment** — composition manifests with automatic rollback on health failures

Every action is recorded in the **append-only Merkle-tree audit log**, signed
with Ed25519, and queryable via `aegisclaw audit log` / `audit why` / `audit verify`.

---

## Project Structure

| Path | Description |
|---|---|
| `cmd/aegisclaw` | Host CLI + TUI entrypoint |
| `cmd/guest-agent` | Agent payload that runs inside Firecracker VMs |
| `internal/` | Core packages (kernel, court, builder, sandbox, audit, composition) |
| `internal/builder/securitygate/` | SAST, SCA, secrets, policy-as-code gates |
| `internal/composition/` | Versioned deployment manifests with rollback |
| `docs/` | Living specs, roadmap, and tutorials |
| `adrs/` | Architecture Decision Records |

## Documentation

- **[First Skill Tutorial](docs/first-skill-tutorial.md)** — step-by-step guide for new users
- **[Product Requirements (PRD)](docs/PRD.md)** — full product vision
- **[CLI Specification](docs/cli-design.md)** — command reference
- **[PRD Deviations](docs/prd-deviations.md)** — alignment status (14 of 16 resolved)
- **[Threat Model](docs/threat-model.md)** — security analysis

See [`adrs/`](adrs/) for Architecture Decision Records.

## Development

```bash
# Run the full test suite
go test ./... -count=1

# Run integration tests only
go test ./cmd/aegisclaw/ -run 'Integration|Journey' -v

# Rebuild after code changes
go build -o aegisclaw ./cmd/aegisclaw
```

## Contributing

Super early — but feedback welcome!
- Read the living docs first
- Open issues for questions, bugs, or ideas

Built with ❤️ in Go — feedback? Drop an issue!
