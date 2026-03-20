# First Run: Hello World Skill

This guide walks you through creating your first AegisClaw skill — a simple
"Hello World" — from scratch. By the end you will have proposed, reviewed,
built, activated, and invoked a skill running inside its own Firecracker
microVM.

## Prerequisites

| Requirement | Why |
|---|---|
| Linux host | Firecracker only runs on Linux |
| Go 1.26+ | Build from source |
| Ollama installed & running | LLM backend for court review and code generation |
| Firecracker + jailer binaries | Sandbox isolation for skills, reviewers, and builders |

Verify Ollama is reachable (default `http://127.0.0.1:11434`):

```bash
curl -s http://127.0.0.1:11434/api/tags | head -c 200
```

> **Model names:** The default persona configs reference `qwen2.5:latest` and
> `llama3.2:latest`. Make sure these models (or equivalents) are pulled in
> Ollama. You can edit the persona files under
> `~/.config/aegisclaw/personas/` to match what you have installed.

## 1 — Build

```bash
git clone https://github.com/PixnBits/AegisClaw.git
cd AegisClaw

# Host CLI
go build -o aegisclaw ./cmd/aegisclaw

# Guest agent (runs inside microVMs)
go build -o guest-agent ./cmd/guest-agent
```

Confirm the build:

```bash
./aegisclaw version
# AegisClaw v0.1.0
```

## 2 — Start the Kernel

The kernel is the long-running process that manages sandboxes, the audit log,
and IPC. Every other command requires it.

```bash
./aegisclaw start
```

You should see:

```
AegisClaw kernel started.
  Message-Hub: running
  IPC Routes: [message-hub]
Press Ctrl+C to stop.
```

> **Tip:** To run the kernel in the background and capture logs:
>
> ```bash
> ./aegisclaw start > aegisclaw.log 2>&1 &
> ```

In a **new terminal**, verify that the kernel is healthy:

```bash
./aegisclaw status
```

## 3 — Propose the Skill

AegisClaw treats every change as a *proposal* that flows through governance.
You can create a proposal either through the interactive wizard or with CLI
flags.

### Option A — Non-interactive (recommended for first run)

Use `--name`, `--tool`, and `--submit` to create and submit in one command:

```bash
./aegisclaw propose skill "Hello World" \
  --name hello-world \
  --tool "greet:Returns a Hello World greeting message" \
  --data-sensitivity 1 --network-exposure 1 --privilege-level 1 \
  --submit
```

```
Proposal created successfully.
  ID:       a1b2c3d4-...
  Title:    Add Hello World skill
  Skill:    hello-world
  Category: new_skill
  Risk:     low
  Status:   draft

Proposal submitted for court review.
  Status:   submitted

Start review: aegisclaw court review a1b2c3d4-...
```

Copy the proposal ID — you will need it for the next steps. With `--submit`
you can skip straight to [Step 5 — Court Review](#5--run-court-review).

> **Available flags:**
>
> | Flag | Purpose |
> |---|---|
> | `--name` | Skill identifier (lowercase, letters/digits/hyphens) |
> | `--title` | Proposal title (default: "Add \<goal\> skill") |
> | `--description` | Skill description |
> | `--tool` | Tool as `name:description` (repeatable) |
> | `--data-sensitivity` | 1–5 (default 1) |
> | `--network-exposure` | 1–5 (default 1) |
> | `--privilege-level` | 1–5 (default 1) |
> | `--allowed-host` | Allowed network host (repeatable) |
> | `--allowed-port` | Allowed network port (repeatable) |
> | `--secret` | Secret reference name (repeatable) |
> | `--submit` | Immediately submit for court review |

### Option B — Interactive wizard

If you omit `--name` and `--tool`, the command launches a full interactive
wizard:

```bash
./aegisclaw propose skill "Hello World"
```

The wizard steps through eight screens. For a minimal Hello World skill, use
the values below — just press Enter to accept a default where one is shown.

### 3.1 — Skill Identity

| Field | Value |
|---|---|
| **Skill Name** | `hello-world` |
| **Proposal Title** | `Add Hello World skill` |
| **Description** | `A minimal greeting skill that returns "Hello, World!"` |
| **Category** | `New Skill` |

### 3.2 — Clarification Questions

| Field | Value |
|---|---|
| External APIs | *(leave empty)* |
| Data handled | `Public, non-sensitive` |
| Frequency | `On-demand (user triggered)` |
| Failure mode | *(leave empty)* |
| Dependencies | *(leave empty)* |

### 3.3 — Risk Assessment

Since this skill has no network access, no secrets, and no elevated
privileges, choose the lowest values:

| Dimension | Value |
|---|---|
| Data Sensitivity | `1 - Public/non-sensitive` |
| Network Exposure | `1 - No network access` |
| Privilege Level | `1 - Read-only workspace` |

### 3.4 — Network Policy

```
Does this skill need network access?  No
```

### 3.5 — Secrets

```
Does this skill need secrets (API keys, tokens)?  No
```

### 3.6 — Court Personas

Accept the default — all five personas selected:

- CISO
- Senior Coder
- Security Architect
- Tester
- User Advocate

### 3.7 — Tool Definition

| Field | Value |
|---|---|
| Tool Name | `greet` |
| Tool Description | `Returns a Hello World greeting message` |
| Add another? | `No` |

### 3.8 — Confirmation

Review the summary and confirm with **Yes**.

The wizard prints:

```
Proposal created successfully.
  ID:       a1b2c3d4-...
  Title:    Add Hello World skill
  Skill:    hello-world
  Category: new_skill
  Risk:     low
  Status:   draft

Submit for review: aegisclaw propose submit a1b2c3d4-...
```

Copy the proposal ID — you will need it for the next steps.

> **Tip:** You can review your proposal at any time with:
>
> ```bash
> ./aegisclaw propose show <proposal-id>
> ```
>
> Or list all proposals:
>
> ```bash
> ./aegisclaw propose ls
> ```

## 4 — Submit for Court Review

> **Skip this step** if you used `--submit` in step 3.

A draft proposal is not yet visible to the governance court. Submit it:

```bash
./aegisclaw propose submit <proposal-id>
```

```
Proposal a1b2c3d4-... submitted for court review.
  Status: submitted

Start review: aegisclaw court review a1b2c3d4-...
```

## 5 — Run Court Review

The court launches five AI personas — each in its own Firecracker sandbox — to
independently review your proposal. Consensus requires ≥ 80 % approval **and**
an average risk score ≤ 7.0.

```bash
./aegisclaw court review <proposal-id>
```

```
Starting court review for proposal a1b2c3d4-...

Court Session: e5f6a7b8-...
  Proposal: a1b2c3d4-...
  State:    approved
  Verdict:  approved
  Risk:     0.2
  Rounds:   1

Round Results:
  Round 1 (consensus=true, avg_risk=0.2):
    PERSONA         VERDICT    RISK   COMMENTS
    ------------------------------------------------------------
    CISO            approve    0.0    Low risk, no data exposure..
    SeniorCoder     approve    0.0    Straightforward implement..
    SecurityArch..  approve    0.0    No network, no secrets, mi..
    Tester          approve    0.2    Testable, single tool, goo..
    UserAdvocate    approve    0.0    Clear purpose, simple inte..
    Risk Heatmap: CISO=0.0, SeniorCoder=0.0, ...
```

A Hello World skill should sail through with a low risk score. Small models
may need more than one round to converge.

> **If the proposal is escalated** (unlikely for Hello World, but possible if
> personas disagree), you can cast a human override:
>
> ```bash
> ./aegisclaw court vote <proposal-id> approve "Manual approval for hello-world demo"
> ```

## 6 — Build the Skill

Once approved, the builder pipeline automatically generates Go source code,
compiles it, runs tests (requiring > 80 % coverage), and commits the result.

You can check builder progress:

```bash
./aegisclaw builder status
```

```
Builder Sandboxes: 1 total, 1 active

ID                                    STATE         PROPOSAL                              STARTED
------------------------------------  ------------  ------------------------------------  --------------------
f9e8d7c6-...                          building      a1b2c3d4-...                          2026-03-19T10:15:00Z
```

When the build completes the state transitions to `done`.

## 7 — Activate the Skill

Activating a skill spins up a dedicated Firecracker microVM, registers the
skill in the Merkle-hashed registry, and makes it available for invocation over
IPC.

```bash
./aegisclaw skill activate hello-world
```

```
Skill 'hello-world' activated.
  Sandbox: d4c3b2a1-... (pid=12345)
  Registry: v1 hash=9a8b7c6d5e4f3a2b
  Root hash: 1122334455667788
```

Verify it shows up in the skill list:

```bash
./aegisclaw skill ls
```

```
NAME                 SANDBOX                              STATE      VER  HASH
hello-world          d4c3b2a1-...                         active     1    9a8b7c6d5e4f3a2b

Registry: seq=1 root=1122334455667788
```

## 8 — Invoke the Skill

Open the interactive chat to invoke your new skill:

```bash
./aegisclaw chat
```

Inside the chat TUI, type a message that exercises the `greet` tool:

```
> Say hello using the greet tool
```

AegisClaw routes the request to the `hello-world` microVM via vsock IPC. The
skill executes the `greet` tool and returns the result:

```
Hello, World!
```

## 9 — Verify the Audit Trail

Every action — proposal creation, court review, build, activation, and
invocation — was signed with Ed25519 and appended to a tamper-evident Merkle
chain. Verify it:

```bash
./aegisclaw audit verify
```

```
Verifying Merkle audit chain: ...
  OK: 12 entries verified
  Chain head: 10c09a04...
```

You can also explore the full audit log interactively:

```bash
./aegisclaw audit explorer
```

## 10 — Clean Up

When you are done experimenting, deactivate the skill and stop the kernel:

```bash
# Deactivate the skill (stops its microVM)
./aegisclaw skill deactivate hello-world

# Stop the kernel (Ctrl+C in the kernel terminal, or kill the background process)
```

## What Just Happened?

Here is the full lifecycle you just walked through:

```
propose skill ──► submit ──► court review ──► build ──► activate ──► invoke
     │                           │                │          │           │
   wizard            5 AI personas          codegen +     microVM     vsock
   (TUI)             in sandboxes           test loop     spun up      IPC
```

1. **Propose** — The interactive wizard collected your skill definition and
   created a governed proposal with a `SkillSpec`.
2. **Submit** — Transitioned the proposal from `draft` to `submitted`.
3. **Court Review** — Five LLM personas (CISO, Senior Coder, Security
   Architect, Tester, User Advocate) each reviewed the proposal independently
   in isolated sandboxes and voted.
4. **Build** — The builder sandbox generated Go code from the `SkillSpec`, compiled
   it, ran tests (>80 % coverage gate), linted, and committed the artifact.
5. **Activate** — A Firecracker microVM was launched with the compiled skill
   code mounted read-only. Network policy (default-deny) was enforced.
6. **Invoke** — The chat TUI sent a request over vsock IPC to the skill's
   microVM, which executed the `greet` tool and returned the result.

Every step was cryptographically signed and appended to the Merkle audit chain.

## Next Steps

- **Add network access** — Try a skill that calls an external API (the wizard
  will prompt you for allowed hosts and ports).
- **Use secrets** — Store API tokens with `./aegisclaw secret add` and
  reference them in a new skill proposal.
- **Explore the TUI dashboard** — Run `./aegisclaw status --tui` for a live
  view of sandboxes, skills, and the audit log.
- **Read the architecture docs** — See `docs/architecture.md` for deep dives
  on the security model, IPC design, and governance engine.
