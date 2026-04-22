# AegisClaw

**Paranoid-by-design, self-evolving local agent platform**

AegisClaw is a local AI agent runtime that treats every boundary between
components as a security boundary. You describe what you want in plain English
(or via CLI) and the system proposes a new "skill", puts it through a
Governance Court of five AI reviewers, runs mandatory security gates, and
deploys the approved code inside a Firecracker microVM — all on your own
machine, without calling any cloud service.

Key features:
- **Governance Court** — 5 AI personas (CISO, SeniorCoder, SecurityArchitect,
  Tester, UserAdvocate) review every proposed code change in isolated microVMs
- **Builder pipeline** — code generation runs inside a sandboxed Firecracker
  microVM; output is committed and signed before activation
- **Mandatory security gates** — SAST, SCA, secrets scanning, and
  policy-as-code run on every build; no bypass mechanism exists
- **Append-only Merkle-tree audit log** — every action is signed with Ed25519
  and queryable via `audit log` / `audit why` / `audit verify` / `audit trace <id>`
- **Versioned deployment** — composition manifests track every deployed skill
  version; unhealthy deployments roll back automatically
- **Web portal + Terminal UI** — interactive chat and live tool-call visibility
  at `http://127.0.0.1:7878`; TUI via `aegisclaw chat`
- **Multi-channel gateway** — receive messages from webhooks, Discord bots,
  Telegram, and other adapters; all routed through the same secured agent loop
- **Workspace customisation** — drop `SOUL.md`, `AGENTS.md`, or `TOOLS.md`
  files in `~/.aegisclaw/workspace/` to tailor the agent's personality and tools
- **Hierarchical multi-agent** — spawn ephemeral Worker agents for research,
  coding, or summarisation; coordinate via async timers and signals
- **ClawHub registry bridge** — browse and import community skills; every
  import is automatically submitted to the Governance Court before activation

The main agent, Governance Court reviewers, the builder, and all skills run
exclusively inside Firecracker microVMs. KVM is a hard requirement — the
daemon will not start without it and there is no fallback mode.

---

## Getting Started

### Prerequisites

| Requirement | Notes |
|---|---|
| **Linux** (x86_64 or aarch64) | Firecracker requires KVM — `/dev/kvm` must be accessible. The daemon will not start without it. |
| **Go 1.25+** | Build from source |
| **Firecracker + jailer** | MicroVM runtime — see install instructions below |
| **Ollama** | LLM inference — install from [ollama.com](https://ollama.com) |

#### Install Firecracker and jailer

Firecracker releases ship a `firecracker` binary and a `jailer` binary. Install
both to `/usr/local/bin/`:

```bash
ARCH=$(uname -m)   # x86_64 or aarch64
FC_VERSION=v1.11.0
curl -fsSL "https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-${ARCH}.tgz" \
  | tar -xz
sudo install -o root -g root -m 0755 "release-${FC_VERSION}-${ARCH}/firecracker-${FC_VERSION}-${ARCH}" /usr/local/bin/firecracker
sudo install -o root -g root -m 0755 "release-${FC_VERSION}-${ARCH}/jailer-${FC_VERSION}-${ARCH}"     /usr/local/bin/jailer
```

Verify KVM access:

```bash
ls -la /dev/kvm      # must exist and be accessible to root
```

#### Install Ollama and pull required models

```bash
curl -fsSL https://ollama.com/install.sh | sh

# Start the Ollama server if it is not already running.
# Skip this line if 'systemctl status ollama' or 'pgrep ollama' shows it's active.
ollama serve &

# Model used by the main chat agent (default: llama3.2:3b)
ollama pull llama3.2:3b

# Model used by Court reviewer personas
ollama pull mistral-nemo
```

> Both models are needed for a full first run. `llama3.2:3b` handles your
> chat messages; `mistral-nemo` drives the five Governance Court reviewers
> that evaluate every proposed skill.

---

### 1. Build

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw
go build -o aegisclaw ./cmd/aegisclaw
go build -o guest-agent ./cmd/guest-agent
```

### 2. Initialize

One-time setup — creates directory structure, Ed25519 keypair, and audit log:

```bash
./aegisclaw init
```

### 3. Build System VM Images (One-Time)

The daemon requires dedicated rootfs images for AegisHub (IPC router) and the
web portal microVM.

```bash
sudo ./scripts/build-rootfs.sh --target=aegishub
sudo ./scripts/build-rootfs.sh --target=portal
sudo ./scripts/build-rootfs.sh --target=guest
sudo ./scripts/build-builder-rootfs.sh /var/lib/aegisclaw/rootfs-templates/builder.ext4
```

### 4. Start the Daemon

```bash
sudo ./aegisclaw start &> aegisclaw.log
```

> **Why sudo?** Firecracker requires root for KVM device access and network
> namespace creation. On first run, `start` automatically downloads the
> Firecracker kernel image and builds the Alpine rootfs template — no manual
> download required. AegisHub and portal rootfs images are built in Step 3.

Verify the daemon is up:

```bash
./aegisclaw status
```

Open the web portal:

```bash
xdg-open http://127.0.0.1:7878
```

### 5. Open Chat

```bash
./aegisclaw chat
```

Type a message or `/help` for available commands.

### 6. Create Your First Skill

In chat, describe what you want in plain English:

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

Every code change goes through:
1. **Governance Court** — 5 AI personas review proposals in isolated Firecracker
   microVMs (requires KVM; daemon fails fast if unavailable)
2. **Builder pipeline** — code generation runs in a sandboxed Firecracker microVM
3. **Security gates** — SAST, SCA, secrets scanning, policy-as-code (mandatory,
   no bypass)
4. **Versioned deployment** — composition manifests with automatic rollback on
   health failures

Every skill runs in its own **Firecracker microVM** with:
- Read-only rootfs
- No network access (unless explicitly declared and approved in the proposal)
- `cap-drop ALL` — no Linux capabilities
- Secrets injected via proxy at runtime (never in code)

Every action is recorded in the **append-only Merkle-tree audit log**, signed
with Ed25519, and queryable via `aegisclaw audit log` / `audit why` / `audit verify` / `audit trace <id>`.

---

## Integrations & Extensibility

AegisClaw gains new capabilities through **skills** — everything goes through the
Governance Court before running. Just ask the agent; it handles the proposal details.

| Integration type | Example ask | What gets proposed |
|---|---|---|
| Messaging | "Add a Discord/Telegram/Slack skill" | Channel adapter with bot-token secret, platform API allowlist, send/receive tools |
| Developer tools | "Add a GitHub skill" | API wrapper with token secret, `github.com` allowlist, PR/issue/review tools |
| Shell automation | "Add a shell scripting tool" | Script runner, no network, `/workspace` sandbox, bash/python/node |
| Voice | "Add voice interaction" | Host-audio proxy adapter, TTS model, wake-word tools |
| Custom webhook | "Forward Slack alerts to me" | Webhook listener skill, inbound-only, event-filter tools |
| Registry | "Install the time-greeter from ClawHub" | Imports and auto-submits to Court |

---

## Workspace Customisation

Drop any of these optional files in `~/.aegisclaw/workspace/` to personalise the
agent without a Court proposal:

| File | Purpose |
|---|---|
| `SOUL.md` | Guiding principles and personality tweaks |
| `AGENTS.md` | Identity overrides (name, role, tone) |
| `TOOLS.md` | Tool preference hints (prefer certain skills) |
| `<skill>.SKILL.md` | Per-skill usage notes injected at build time |

---

## Project Structure

| Path | Description |
|---|---|
| `cmd/aegisclaw` | Host CLI, TUI, daemon, and gateway entrypoint |
| `cmd/guest-agent` | Agent binary that runs inside Firecracker VMs |
| `internal/` | Core packages (kernel, court, builder, sandbox, audit, composition) |
| `internal/builder/securitygate/` | SAST, SCA, secrets, policy-as-code gates |
| `internal/composition/` | Versioned deployment manifests with rollback |
| `internal/gateway/` | Multi-channel gateway (webhook, Discord, Telegram adapters) |
| `internal/provision/` | Automatic Firecracker kernel + rootfs provisioning |
| `internal/worker/` | Ephemeral Worker agent management |
| `internal/registry/` | ClawHub registry bridge |
| `docs/` | Living specs, roadmap, and tutorials |
| `adrs/` | Architecture Decision Records |

## Documentation

- **[First Skill Tutorial](docs/first-skill-tutorial.md)** — step-by-step guide for new users
- **[Architecture](docs/architecture.md)** — component interaction model and north-star design
- **[Product Requirements (PRD)](docs/PRD.md)** — full product vision
- **[CLI Specification](docs/cli-design.md)** — command reference
- **[PRD Deviations](docs/prd-deviations.md)** — alignment status and open items
- **[Threat Model](docs/threat-model.md)** — security analysis

See [`adrs/`](adrs/) for Architecture Decision Records.

## Development

See **[CONTRIBUTING.md](CONTRIBUTING.md)** for the full development guide:

- Running tests (unit, journey, golden trace, in-process)
- In-process integration test executor (⚠️ test-only, no KVM required)
- Golden trace snapshot testing
- Security model and build-tag rules
- How to submit changes

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow, testing
guide, security rules, and how to submit a pull request.

Feedback welcome — open issues for questions, bugs, or ideas!

Built with ❤️ in Go
