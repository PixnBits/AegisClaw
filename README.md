# SeedClaw – Paranoid, Local-First, Self-Bootstrapping AI Agent Platform

**Zero-trust. Zero-cloud. Zero-binaries-from-strangers.**  
Bootstrap your own auditable agent swarm from markdown prompts only.  
Everything runs in Docker containers with strict least-privilege networking and mounts.  
Audit trail is append-only and trivially greppable.

**Tagline (2026):**  
“Run one tiny Go binary → paste a paranoid bootstrap prompt → watch it write, sandbox and register its own skills forever. No vendor lock-in. No surprises.”

## Core Security Posture (non-negotiable)

- Trusted Computing Base = only `seedclaw` binary + 5 core skills (message-hub, llm-caller, ollama, coder, user-agent)
- Every generated skill is sandbox-compiled, statically analyzed, and registered with **explicit** `network_policy` (default: no outbound internet)
- All inter-skill communication routes exclusively through `message-hub` (no direct container ↔ container TCP)
- User-agent skill **always** runs a threat-model phase before tool use → high-risk actions require explicit user “YES”
- Audit log (`shared/audit/seedclaw.log`) shows every network decision, mount, and safety gate

## Prerequisites

1. **Docker** installed and running (Docker Desktop or docker.io / docker-ce)
   - Linux: `docker compose` plugin or standalone `docker-compose`
   - macOS / Windows: Docker Desktop ≥ 4.15
2. **Enough RAM & disk**  
   - ≥ 16 GB RAM recommended (Ollama + 7B–32B models are memory hungry)  
   - ≥ 40–60 GB free disk (models + temporary build artifacts)
3. **Ollama models** (strongly recommended to avoid cold-start delays)
   - Copy existing models from `~/.ollama/models` or `/usr/share/ollama/.ollama/models` (Linux/macOS) or `%USERPROFILE%\.ollama\models` (Windows)  
     into `./shared/ollama/models` **before** first run  
   - Minimal working set:  
     `qwen2.5-coder:7b` or `qwen2.5:14b` (fast local coding)  
     `llama3.1:8b` or `phi4:14b` (general reasoning + safety auditor)

## Quick Start (first run in ~10–20 minutes)

### Step 1 – Clone & prepare

```bash
git clone https://github.com/PixnBits/SeedClaw.git
cd SeedClaw
```

Copy Ollama models (if you have them already):

```bash
# Example – Linux/macOS
cp -r ~/.ollama/models ./shared/ollama/models
```

### Step 2 – Generate the seedclaw binary

Use a strong coding LLM (Claude 3.5 Sonnet, o1, Gemini 1.5 Pro, Cursor, Aider, etc.) with the file `./bootstrap-prompt.md`.

1. Open `./bootstrap-prompt.md` in your editor
2. Copy the entire content
3. Paste into your preferred coding agent / chat interface

The coding LLM should generate the source code needed to start.

### Step 3 – Build & run

```bash
go mod tidy
go build -o seedclaw ./seedclaw.go
chmod +x ./seedclaw
```

Start SeedClaw (first run will pull/build core containers):

```bash
./seedclaw
```

You should see:

- Docker network `seedclaw-net` created
- Core skills starting (`message-hub`, `llm-caller`, `ollama`, `coder`, `user-agent`)
- Prompt appears: `>`

### Step 4 – First interactions (examples)

**Hello World in Go**

```
> Please write a hello world example program in Go.
```

Expected flow:

1. user-agent → threat model phase → usually LOW risk → proceeds automatically or asks for confirmation
2. user-agent → llm-caller → coder skill
3. coder → generates Go file + Dockerfile + SKILL.md + registration metadata
4. seedclaw → vets, compiles in sandbox, registers skill
5. output appears in terminal (code + path to artifact)

**Build a weather skill**

```
> Create a new skill called "weather" that can fetch current weather for a city using a free weather API. Use only HTTPS to approved domains. No persistent storage needed.
```

Expected:

- user-agent threat-models the request (medium risk → network outbound)
- shows concerns + “PROCEED? (YES/NO)”
- type `YES`
- coder generates skill with narrow `network_policy` → e.g. `domains: ["api.open-meteo.com"]`
- seedclaw rejects anything using `network_mode: host` or undeclared outbound
- new skill appears in registry → you can now say “weather Phoenix, AZ”

## How to audit what’s really happening

```bash
# See every network permission ever granted
grep -E '"network_policy|outbound|domains|network_mode"' shared/audit/seedclaw.log

# See safety gate decisions
grep -E 'threat_model|risk|PROCEED|confirmation|injection|safety_violation' shared/audit/seedclaw.log

# Current compose configuration
cat compose.yaml
```

## Philosophy & Non-Goals (MVP)

- No multi-user, no auth (yet)
- No GUI (terminal REPL only)
- No persistent memory beyond registry + audit log
- No cloud / hosted mode
- No skill can talk directly to another skill or the internet unless explicitly declared & audited

## Contributing

Want tighter safety prompts? Better rejection rules? Faster bootstrap?  
Submit a PR that only touches markdown files (prompts, SKILL.md, ARCHITECTURE.md, etc.).  
Code changes must come from generated/registered skills — never direct commits to core files.

For working with a web-based LLM for document editing (not code):
```shell
$ echo "https://github.com/PixnBits/SeedClaw/tree/third" > REPO_SUMMARY.md && echo "=== FULL FILE LIST ===" >> REPO_SUMMARY.md && git ls-files >> REPO_SUMMARY.md && echo -e "\n\n=== KEY FILES ===\n" >> REPO_SUMMARY.md && for f in $(git ls-files './*.md'); do echo -e "\n\n==== $f ====\n" >> REPO_SUMMARY.md; cat "$f" >> REPO_SUMMARY.md; done && for f in $(git ls-files '*SKILL.md'); do echo -e "\n\n==== $f ====\n" >> REPO_SUMMARY.md; cat "$f" >> REPO_SUMMARY.md; done
```

## License
MIT – do whatever. Just don't blame us if your agent starts ordering pizza at 3 AM.

Happy paranoid bootstrapping.
