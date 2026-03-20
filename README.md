# AegisClaw

**Paranoid-by-design, self-evolving local agent platform**

Secure, local-first AI agent runtime with:
- Firecracker microVM isolation for untrusted code/agents
- Governance Court SDLC (structured, auditable decision-making?)
- Currently Ollama-only on Linux
- Terminal UI (TUI) for interactive ReAct-style chat & agent control

## Quick Highlights
- Host CLI + guest agent in microVMs
- Bubble Tea-powered TUI for agent interaction
- Structured skills via `SkillSpec` + code generation prompts
- Hardening, tests, and first-run setup already in place

## Living Documents
These Markdown files are the single source of truth for vision, design, and progress — they evolve frequently.

- **[Product Requirements (PRD)](docs/PRD.md)**  
- **[Architecture Specification](docs/architecture.md)**  
- **[Human Interface Design](docs/design.md)**  
- **[Epics & Roadmap](docs/epics.md)** — current focus: completing Epic 6  
- **[Task Breakdown](docs/tasks.md)**  

See [`adrs/`](adrs/) for Architecture Decision Records (rationale for major choices).

## Installation (From Source – Currently the Only Way)

Prerequisites:
- Go 1.26+ (or latest stable)
- Linux host (Ollama + Firecracker dependencies)
- Ollama installed & running locally

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw

# Build the main host CLI
go build -o aegisclaw ./cmd/aegisclaw

# Build the guest agent (runs inside microVMs)
go build -o guest-agent ./cmd/guest-agent

# Or install to $GOPATH/bin
go install ./cmd/aegisclaw
go install ./cmd/guest-agent
```

**Note**: Deployment/hardening scripts are in `deploy/` — see `docs/` and recent commits for setup details. First-run and test automation exist.

## Quick Start / Usage

Quick start and common usage examples — copy/paste to try locally.

Prerequisites
- Linux host
- Go 1.26+ to build from source
- Ollama installed and running locally (default: http://127.0.0.1:11434)
- Firecracker and required tooling available for sandboxed reviewer/builder execution

Build

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw
go build -o aegisclaw ./cmd/aegisclaw
go build -o guest-agent ./cmd/guest-agent
```

Run (host)

```bash
# Start the host agent with the TUI (interactive ReAct/chat)
./aegisclaw

# Run non-interactive commands and capture logs
Use specific subcommands for non-TUI operations (for example, `start` to run the kernel
or `status --tui` to launch the TUI dashboard). To capture logs, redirect stdout/stderr:

```bash
# Start kernel and capture logs to a file
./aegisclaw start > aegisclaw.log 2>&1 &

# Launch the interactive status dashboard (TUI)
./aegisclaw status --tui

# Or run directly from source and capture logs
go run ./cmd/aegisclaw start > aegisclaw.log 2>&1 &
```
```

Notes
- The TUI opens an interactive chat-like interface for controlling the agent and submitting proposals.
- The `guest-agent` binary is intended to run inside Firecracker microVMs and is not invoked directly on the host.
- Ollama must be running and reachable from the reviewer/builder sandboxes (default localhost:11434). The platform enforces sandbox isolation: host/kernel processes are not permitted to call Ollama directly.

Note about commands: If you built a previous binary or pulled new code, the CLI may have changed. If you see "unknown command", rebuild the binary or run directly from source:

```bash
# Rebuild the binary to pick up new commands
go build -o aegisclaw ./cmd/aegisclaw
./aegisclaw model list

# Or run without building the binary
go run ./cmd/aegisclaw model list
```

Model management (local Ollama models)

```bash
# List registered models and their status
./aegisclaw model list

# Verify a specific model (checks digest/availability)
./aegisclaw model verify <model-name>

# Pull/update a model from the Ollama store
./aegisclaw model update <model-name>
```

Developer / testing

```bash
# Run full unit test suite
go test ./... -count=1
```

If you want me to expand this into a short "First Run" script or add distro-specific installation steps for Ollama/Firecracker, I can add that next.

## Project Structure Overview
- `cmd/aegisclaw`       → Main host CLI + TUI entrypoint  
- `cmd/guest-agent`     → Agent payload that runs inside Firecracker VMs  
- `internal/`           → Core packages (models, services, orchestration)  
- `config/`             → SkillSpec, CodeGenerator, prompt logic  
- `deploy/`             → Hardening, tests, first-run setup  
- `docs/`               → Living specs & roadmap  
- `adrs/`               → Decision records  

## Contributing
Super early — but feedback welcome!  
- Read the living docs first  
- Open issues for questions, bugs, or ideas   

Built with ❤️ in Go — feedback? Drop an issue!
