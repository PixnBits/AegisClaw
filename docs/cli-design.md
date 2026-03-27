### AegisClaw CLI Specification

**Document**: CLI Interface
**Project**: AegisClaw v2.0 – Secure-by-Design Local Agent Platform  
**Status**: Ready for implementation  
**Alignment**: Fully traceable to the AegisClaw PRD (especially Sections 10, 13.2, and 13.3)

#### 1. Overall Philosophy
- Keep the CLI **minimal and approachable** for hobbyists while providing precise control for power users and enterprises.
- **Chat is the primary daily interface** — most operations should be doable via natural language inside `aegisclaw chat`.
- **Safe Mode** is a strict minimal recovery environment with no LLM, no Court, and no skills running.
- Everything important is git-backed and recorded in the append-only Merkle-tree audit log.
- Secrets are **never** accepted or displayed via chat — only through dedicated CLI commands.
- High-risk actions always require explicit confirmation (unless `--force` is used, with full audit trail).

#### 2. Top-Level Commands

The main binary is **`aegisclaw`**.

```bash
Usage: aegisclaw [command] [flags]

Core Commands:
  init          Initialize AegisClaw (first-time setup)
  start         Start the coordinator daemon
  stop          Gracefully stop the daemon
  status        Show system status and health
  chat          Enter interactive chat with the main agent (primary daily interface)
  skill         Manage skills (add, list, revoke, info)
  audit         Query the append-only audit log
  secrets       Manage secrets (add, list, rotate) — never exposes values
  self          Self-improvement and system management proposals
  version       Show version and build information

Global Flags:
  --json        Output in structured JSON (for scripting)
  --verbose, -v Increase verbosity
  --dry-run     Simulate action without making changes
  --force       Skip confirmations (logged in audit trail)
```

#### 3. Detailed Command Specifications

**`aegisclaw init`**
```bash
aegisclaw init [--profile hobbyist|startup|enterprise] [--strictness high|medium|low]
```
- One-time setup: creates `~/.aegisclaw/` directory structure, git repo, Merkle-tree audit log, and initial composition manifest.
- Prompts for user profile and suggests appropriate strictness level.
- Verifies Ollama availability and model hashes.

**`aegisclaw start`**
```bash
aegisclaw start [--safe] [--background]
```
- Starts the MicroVM Coordinator Daemon.
- `--safe`: Enters **Safe Mode** (detailed below).
- Normal mode starts coordinator + main agent sandbox + Court (as configured).

**`aegisclaw stop`**
- Gracefully shuts down all microVMs and the coordinator.
- Always logs the shutdown event.

**`aegisclaw status`**
- Shows running microVMs, active skills, resource usage, composition version, and health summary.
- Supports `--json`.

**`aegisclaw chat`**
- Primary interactive REPL with the main agent sandbox.
- Users interact via natural language, for example:
  - "Add Slack messaging capability"
  - "Revoke the skill I added yesterday"
  - "Explain why you performed that action"
  - "Propose a performance improvement for skill addition"
- High-risk actions trigger explicit confirmation prompts.
- If no subcommand is given (`aegisclaw`), it may default to starting the daemon (if not running) and entering chat.

**`aegisclaw skill`**
```bash
aegisclaw skill list [--json]
aegisclaw skill add "<natural language description>" [--non-interactive]
aegisclaw skill revoke <skill-id> [--reason "<text>"] [--force]
aegisclaw skill info <skill-id>
```
- `skill add` triggers the full Governance Court review process.
- `revoke` is also available conversationally inside chat.

**`aegisclaw audit`**
```bash
aegisclaw audit log [--since <time>] [--skill <id>] [--limit N] [--json]
aegisclaw audit why <action-id>          # Explain why this happened
aegisclaw audit verify                   # Verify Merkle-tree integrity
```

**`aegisclaw secrets`**
```bash
aegisclaw secrets add <name>             # Secure prompt, never echoes the value
aegisclaw secrets list [--json]          # Shows names only, never values
aegisclaw secrets rotate <name>
```

**`aegisclaw self`**
```bash
aegisclaw self propose "<description>"   # Starts Court-reviewed self-improvement
aegisclaw self status
```

**`aegisclaw version`**
- Displays version, git commit, build date, and SBOM summary if available.

#### 4. Safe Mode Specification

When started with `aegisclaw start --safe`:

**What runs**:
- Only the MicroVM Coordinator Daemon
- Thin CLI interface
- Append-only Merkle-tree Audit Log Store (read-only for inspection)

**What does NOT run**:
- Main Agent Sandbox (no chat)
- Governance Court reviewer microVMs
- Any per-skill execution microVMs
- Builder Sandbox
- Secrets Proxy
- Any LLM / Ollama instances

**Safe Mode Banner** (clean version):

```text
╔══════════════════════════════════════════════════════════════╗
║                    AEGISCLAW SAFE MODE                       ║
╚══════════════════════════════════════════════════════════════╝

Minimal recovery environment active. 
No skills, no Court, no main agent sandbox.
Type 'help' for available commands.
```

**Safe Mode Prompt**:
```
AegisClaw Safe Mode >
```

**Available Commands in Safe Mode** (strictly limited):

| Command                        | Description                                                                 |
|--------------------------------|-----------------------------------------------------------------------------|
| `status`                       | Show current running microVMs, composition version, and basic health       |
| `logs [--since <time>] [--limit N]` | Display recent entries from the append-only audit log                     |
| `revoke <skill-id> [--force]`  | Force shutdown + remove a specific skill microVM + git revert. Requires confirmation unless `--force`. |
| `inspect <component>`          | Low-level inspection (`composition`, `vmconfig <id>`, `manifest`)          |
| `recover`                      | Attempt to exit safe mode and restart in normal operation (runs checks first) |
| `stop`                         | Gracefully shut down the coordinator daemon                                |
| `help`                         | Show this command list                                                     |
| `exit` / `quit`                | Alias for `stop`                                                           |

**Safe Mode Rules**:
- No natural language parsing or LLM involvement.
- All actions are logged to the audit log.
- `recover` performs basic health checks; if issues are found, it stays in safe mode and explains why.
- `revoke` is the only destructive command and must always produce a clear audit trail.

#### 5. General Behavior Rules (Mandatory for All Modes)
- All state-changing commands must be recorded in the append-only Merkle-tree audit log.
- Structured JSON output (`--json`) must be supported on all commands.
- High-risk operations require confirmation unless `--force` is used.
- Secrets handling is strictly CLI-only — never via chat.
- If the daemon is not running, commands that need it should suggest `aegisclaw start` first.
- Help text (`--help`) should include short security reminders where relevant.

#### 6. User Experience Guidelines
- **Hobbyist flow**: `aegisclaw start` → `aegisclaw chat` (or just `aegisclaw`)
- **Power user / enterprise flow**: Use explicit subcommands (`skill`, `audit`, `secrets`, etc.) for precision or scripting.
- Safe mode should feel deliberate and sterile — it is an emergency recovery tool, not a daily interface.
