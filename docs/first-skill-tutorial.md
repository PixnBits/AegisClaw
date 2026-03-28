# Tutorial: Creating Your First Skill

This guide walks you through creating your first AegisClaw skill from scratch.
By the end you will have proposed, reviewed, built, and activated a real skill —
and you'll understand the security architecture that makes it safe.

## The Example Skill

We'll build a **time-of-day greeter**: a skill that says hello to the user with
a message appropriate for the time of day ("good morning", "good evening", etc.),
respecting DST, in en-US.

User request in chat:

> please add a skill that says hello to the user with a message appropriate for
> the time of day ("good morning", "good evening", etc.) respecting DST, in en-US

---

## Prerequisites

| Requirement | Why |
|---|---|
| **Linux host** (x86_64) | Firecracker microVMs require KVM on Linux |
| **Go 1.26+** | Build from source |
| **Ollama** running locally | LLM inference for Court reviewers and the main agent |
| **Firecracker + jailer** | MicroVM isolation (optional for first exploration — falls back to direct mode without `/dev/kvm`) |

### Install Ollama (if not already running)

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama serve &              # start in background
ollama pull mistral-nemo    # pull a model for Court review
```

### Install AegisClaw

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw

# Build the host CLI and guest agent
go build -o aegisclaw ./cmd/aegisclaw
go build -o guest-agent ./cmd/guest-agent

# Verify the build
./aegisclaw version
```

---

## Step 1 — Initialize

Run `aegisclaw init` to create the directory structure, keypair, and audit log:

```bash
./aegisclaw init --profile hobbyist
```

Output:

```
Initializing AegisClaw...
  Profile:    hobbyist
  Strictness: low
  Directory:  /home/you/.aegisclaw

AegisClaw initialized successfully.
  Public Key: a5b4a8c1...

Next steps:
  aegisclaw start       # Start the coordinator daemon
  aegisclaw chat        # Enter interactive chat
```

**What happened:**
- Created `~/.config/aegisclaw/` (config, personas, secrets directories)
- Created `~/.local/share/aegisclaw/` (audit log, proposals, sandboxes, registry, builder)
- Generated an Ed25519 keypair for signing audit entries
- Wrote the first entry to the Merkle-tree audit log

---

## Step 2 — Start the Daemon

The coordinator daemon manages microVMs, runs the Governance Court, and handles
builder pipelines. Start it in the background:

```bash
sudo ./aegisclaw start &
```

> **Why sudo?** Firecracker requires root for KVM device access and network
> namespace creation. If `/dev/kvm` is unavailable the daemon falls back to
> direct execution mode (suitable for development).

Verify it's running:

```bash
./aegisclaw status
```

---

## Step 3 — Enter Chat

Open the interactive TUI chat. This is your primary interface:

```bash
./aegisclaw chat
```

You'll see the AegisClaw ReAct Chat interface. Type `/help` to see available
commands.

---

## Step 4 — Request the Skill (Chat Path)

In the chat, type the skill request naturally:

```
please add a skill that says hello to the user with a message appropriate
for the time of day ("good morning", "good evening", etc.) respecting DST,
in en-US
```

The main agent will:
1. **Parse your intent** — understand you want a new skill
2. **Gather details** — ask clarifying questions if needed
3. **Create a draft proposal** with a `SkillSpec` including:
   - Skill name: `time-of-day-greeter`
   - Tool: `greet` — returns a locale-aware, DST-respecting greeting
   - Risk assessment: low (no network, no secrets, no privileged ops)
4. **Submit for Court review** — transitions the proposal to `submitted`

### What the agent creates behind the scenes

The agent calls `create_draft` with fields like:

```json
{
  "title": "Add time-of-day greeter skill",
  "description": "A skill that greets the user with a time-appropriate message (good morning, good afternoon, good evening, good night) based on the current local time, respecting DST, in en-US locale.",
  "skill_name": "time-of-day-greeter",
  "tools": [
    {
      "name": "greet",
      "description": "Returns a locale-aware, DST-respecting greeting appropriate for the current time of day (e.g. 'Good morning!', 'Good evening!')"
    }
  ],
  "data_sensitivity": 1,
  "network_exposure": 1,
  "privilege_level": 1
}
```

Then calls `submit` to send it for Court review.

---

## Step 4 (Alternative) — Request the Skill (CLI Path)

If you prefer the CLI over chat, use `skill add` with flags:

```bash
./aegisclaw skill add "time-of-day greeter" \
  --non-interactive \
  --name time-of-day-greeter \
  --tool "greet:Returns a locale-aware DST-respecting greeting appropriate for the current time of day" \
  --data-sensitivity 1 \
  --network-exposure 1 \
  --privilege-level 1
```

This creates and auto-submits the proposal in a single step.

Output:

```
Skill proposal created and submitted for review.
  ID:       a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Title:    Add time-of-day greeter skill
  Skill:    time-of-day-greeter
  Risk:     low
  Status:   submitted
```

---

## Step 5 — Governance Court Review

Once submitted, the Governance Court reviews the proposal automatically. The
Court consists of five AI personas, each with a specialized focus:

| Persona | Focus | Weight |
|---|---|---|
| **CISO** | Security posture, threat model | 0.30 |
| **SeniorCoder** | Code quality, maintainability | 0.30 |
| **SecurityArchitect** | Architecture, attack surface | 0.20 |
| **Tester** | Test coverage, edge cases | 0.10 |
| **UserAdvocate** | Usability, user experience | 0.10 |

Each persona evaluates the proposal and returns a structured verdict:

```json
{
  "verdict": "approve",
  "risk_score": 1.5,
  "evidence": [
    "No network access required",
    "No secrets needed",
    "Read-only time data only",
    "Low privilege level"
  ],
  "comments": "Minimal attack surface. Approve."
}
```

The Court uses **weighted consensus**: if the weighted average of approvals
exceeds the threshold (default: 60%) and the average risk score is below the
max (default: 7.0), the proposal is approved.

For our low-risk greeter skill, approval is near-certain.

### Check review status

```bash
# In chat:
/status

# Or via CLI:
./aegisclaw status
```

If the Court has questions (verdict: `ask`), it will add clarifying questions
and the proposal enters another review round. If rejected, you'll see the
reasons and can revise.

### Human override

If the Court escalates (mixed verdicts or close-call risk), you can vote directly:

```bash
# In chat — the agent will show the proposal and ask for your decision.

# Or via CLI directly:
./aegisclaw court vote <proposal-id> approve "Reviewed manually, acceptable risk"
```

---

## Step 6 — Builder Pipeline

Once approved, the builder pipeline runs automatically:

1. **Launch builder sandbox** — a Firecracker microVM for code generation
2. **Generate code** — LLM produces the skill implementation
3. **Security gates (D8)** — mandatory, cannot be bypassed:
   - **SAST** — static analysis for security anti-patterns (weak crypto, command injection, hardcoded creds)
   - **SCA** — dependency scanning for banned/vulnerable packages
   - **Secrets scanning** — detects accidentally embedded keys/tokens
   - **Policy-as-code** — validates isolation invariants (no host FS, no undeclared network, no privileged ops)
4. **Git commit** — code committed to proposal branch with file hashes
5. **Diff generation** — for Court reviewers to inspect

The pipeline **fails automatically** if any security gate finds error-level or
critical-level issues. There are no bypass mechanisms.

---

## Step 7 — Activation & Invocation

After the builder pipeline succeeds, the skill artifact is signed and
registered. It runs in its own Firecracker microVM with enforced isolation:

- **Read-only rootfs** — the microVM filesystem is immutable
- **No network** — unless explicitly declared and approved
- **cap-drop ALL** — no Linux capabilities
- **Secrets via proxy** — injected at runtime, never in code

### Invoke the skill

In chat, simply ask:

```
What time is it? Say hello!
```

The agent routes to the `time-of-day-greeter` skill's `greet` tool and returns:

```
Good morning! 🌅
```

(Or "Good afternoon!", "Good evening!", "Good night!" depending on the time.)

---

## What Happens Under the Hood

Here's the complete flow for a skill creation, showing the security architecture:

```
User (chat or CLI)
  │
  ▼
┌──────────────────┐
│  aegisclaw chat   │  ← Thin TUI client (D2: no direct LLM calls)
│  or skill add     │
└────────┬─────────┘
         │ Unix socket API
         ▼
┌──────────────────┐
│  Daemon (root)    │  ← Coordinator: manages all microVMs
│  /run/aegisclaw   │
│  .sock            │
└────────┬─────────┘
         │
    ┌────┴────────────────────────────────┐
    │                                     │
    ▼                                     ▼
┌──────────┐                     ┌──────────────┐
│ Proposal  │                     │ Main Agent   │
│ Store     │                     │ Sandbox (D2) │
│ (git-     │                     │ (Firecracker)│
│  backed)  │                     └──────────────┘
└────┬─────┘
     │ submitted
     ▼
┌──────────────────┐
│ Governance Court  │  ← 5 AI personas in microVMs (D1)
│ (Firecracker)     │
│  CISO, Senior-    │
│  Coder, SecArch,  │
│  Tester, UserAdv  │
└────────┬─────────┘
         │ approved
         ▼
┌──────────────────┐
│ Builder Pipeline  │  ← Code generation in microVM
│ (Firecracker)     │
│                   │
│ ┌──────────────┐  │
│ │Security Gates│  │  ← D8: SAST, SCA, secrets, policy
│ │ (mandatory)  │  │
│ └──────────────┘  │
└────────┬─────────┘
         │ artifact signed
         ▼
┌──────────────────┐
│ Composition       │  ← D10: versioned manifest, auto-rollback
│ Manifest          │
└────────┬─────────┘
         │ deployed
         ▼
┌──────────────────┐
│ Skill microVM     │  ← Read-only rootfs, no network, cap-drop ALL
│ (Firecracker)     │
│ time-of-day-      │
│ greeter           │
└──────────────────┘
```

### Audit trail

Every action is recorded in the append-only Merkle-tree audit log:

```bash
# View recent audit entries
./aegisclaw audit log --limit 10

# Explain why an action was performed
./aegisclaw audit why <action-id>

# Verify the entire chain
./aegisclaw audit verify
```

---

## Troubleshooting

### "Is the daemon running?"

Most commands require the daemon. Start it with:

```bash
sudo ./aegisclaw start
```

### "unknown command"

Rebuild after pulling new code:

```bash
go build -o aegisclaw ./cmd/aegisclaw
```

### Court review is slow

Court review involves multiple LLM calls (one per persona). On slow hardware
this can take a minute. Check progress with `./aegisclaw status`.

### Security gate failures

If the builder pipeline fails on security gates, the proposal is not deployed.
Check the failure details and revise the proposal. Common blockers:

- Hardcoded credentials → use `aegisclaw secrets add` instead
- Weak crypto → use `crypto/sha256` or stronger
- Host filesystem access → use `/workspace` only
- Undeclared network access → add `--allowed-host` flags

---

## Next Steps

- **Add more skills** — try a skill that needs network access or secrets
- **Explore safe mode** — `aegisclaw start --safe` for a minimal recovery env
- **Review the audit log** — `aegisclaw audit log` to see all actions
- **Read the PRD** — `docs/PRD.md` for the full product vision
- **Check deviations** — `docs/prd-deviations.md` for what's resolved vs. future work
