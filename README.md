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

Basic run (exact flags TBD — check TUI on launch):

```bash
# Start the host agent with TUI (ReAct/chat interface)
./aegisclaw
```

The TUI should launch a chat-like interface for interacting with the agent.  
Guest agent binary is for microVM payload — not run directly.

More examples coming as Epic 6+ land (multi-agent, skills execution, etc.).

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
