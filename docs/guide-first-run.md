# First Run: Hello World Skill

This guide walks you through creating your first AegisClaw skill — a simple
"Hello World" — from scratch. By the end you will have proposed, reviewed,
built, activated, and invoked a skill running inside its own Firecracker
microVM.

## Prerequisites

| Requirement | Why |
|---|---|
| Linux host with KVM | Firecracker only runs on Linux with `/dev/kvm` |
| Go 1.26+ | Build from source |
| Ollama installed & running | LLM backend for court review and code generation |
| Firecracker + jailer binaries in `/usr/local/bin/` | Sandbox isolation ([install guide](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md)) |
| `e2fsprogs` package | First-run rootfs build (`mkfs.ext4`) |

Verify Ollama is reachable (default `http://127.0.0.1:11434`):

```bash
curl -s http://127.0.0.1:11434/api/tags | head -c 200
```

> **Models:** The default persona configs use `mistral-nemo` and `llama3.2:3b`.
> When you run `aegisclaw court review`, AegisClaw will check whether these
> models are available in Ollama and offer to pull any that are missing.

> **No manual rootfs or kernel setup required.** The daemon automatically
> downloads a Firecracker-compatible vmlinux kernel and builds a minimal
> Alpine rootfs on first start. See [Step 2](#2--start-the-kernel) below.

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

The kernel is the long-running daemon that manages sandboxes, the audit log,
and IPC. It needs root privileges for Firecracker (tap devices, jailer,
nftables). Every other command communicates with it over a Unix socket and
does **not** require root.

```bash
sudo ./aegisclaw start
```

> **Safe Mode:** If you are recovering from a problematic skill or runaway
> tool invocation, start the daemon in safe mode to prevent any skills from
> activating:
>
> ```bash
> sudo ./aegisclaw start --safe-mode
> ```
>
> In safe mode, `skill.activate` and `skill.invoke` requests are rejected.
> Use `/safe-mode off` in the chat TUI (or restart without `--safe-mode`) to
> re-enable normal operation.

On first run the daemon automatically provisions any missing assets:

```
Checking Firecracker assets...
  Downloading vmlinux kernel for x86_64...
  vmlinux kernel ready.
  Building rootfs template (Alpine + guest-agent)...
    Downloading Alpine v3.21 minirootfs...
    Installing guest-agent as init...
  rootfs template ready.
AegisClaw kernel started.
  Message-Hub: running
  IPC Routes: [message-hub]
  API Socket: /run/aegisclaw.sock
Press Ctrl+C to stop.
```

Subsequent starts skip the provisioning step and are near-instant.

> **Tip:** To run the kernel in the background and capture logs:
>
> ```bash
> sudo ./aegisclaw start > aegisclaw.log 2>&1 &
> ```

In a **new terminal**, verify that the kernel is healthy:

```bash
./aegisclaw status
```

## 3 — Propose the Skill

AegisClaw treats every change as a *proposal* that flows through governance.
You can create a proposal in three ways:

- **Option A** — Non-interactive CLI flags (recommended for first run)
- **Option B** — Interactive wizard TUI
- **Option C** — Conversational via `aegisclaw chat`

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

### Option C — Conversational (via chat)

Open the chat TUI and describe what you want. The assistant will ask
clarifying questions, build the proposal incrementally, and save a draft you
can continue later.

```bash
./aegisclaw chat
```

```
> /propose Hello World
```

Or simply describe what you need:

```
> I want to create a skill that returns a greeting message
```

The assistant will walk you through the required fields (skill name, tools,
risk assessment, network/secrets) and present the complete proposal for your
approval before submitting it to the court.

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

The court launches five AI personas to independently review your proposal.
Consensus requires ≥ 80 % approval **and** an average risk score ≤ 7.0.

```bash
./aegisclaw court review <proposal-id>
```

The CLI sends the review request to the running kernel daemon over its Unix
socket. Each persona's LLM call is cross-verified across multiple models.

```
Starting court review for proposal a1b2c3d4-...

Court Session: e5f6a7b8-...
  Proposal: a1b2c3d4-...
  State:    approved
  Verdict:  approved
  Risk:     2.1
  Rounds:   1

Round Results:
  Round 1 (consensus=true, avg_risk=2.1):
    PERSONA         VERDICT    RISK   COMMENTS
    ------------------------------------------------------------
    CISO            approve    2.0    Low risk, no data exposure..
    SeniorCoder     approve    2.0    Straightforward implement..
    SecurityArch..  approve    2.5    No network, no secrets, mi..
    Tester          approve    2.0    Testable, single tool, goo..
    UserAdvocate    approve    2.0    Clear purpose, simple inte..
    Risk Heatmap: CISO=2.0, SeniorCoder=2.0, ...
```

With larger models, a Hello World skill typically sails through with a low
risk score. Smaller models (e.g. `llama3.2:3b`) may take multiple rounds or
escalate to human review.

> **If the proposal is escalated** (common with small models), cast a human
> override:
>
> ```bash
> ./aegisclaw court vote <proposal-id> approve "Manual approval for hello-world demo"
> ```
>
> ```
> Vote recorded.
>
> Court Session: ...
>   State:    approved
>   Verdict:  approved
> ```

## 6 — Build the Skill

> **Status:** The builder pipeline (LLM-driven code generation, compile, test)
> runs inside Firecracker sandboxes and is not yet triggered automatically
> after approval. This section describes the planned flow.

Once approved, the builder pipeline will automatically generate Go source code,
compile it, run tests (requiring > 80 % coverage), and commit the result.

You can check builder progress:

```bash
./aegisclaw builder status
```

```
Builder Sandboxes: 0 total, 0 active
```

## 7 — Activate the Skill

> **Status:** Skill activation and invocation depend on the builder output
> (Step 6). These steps are described here for completeness but require the
> builder pipeline to be wired end-to-end.

Activating a skill spins up a dedicated Firecracker microVM, registers the
skill in the Merkle-hashed registry, and makes it available for invocation over
IPC.

```bash
./aegisclaw skill activate hello-world
```

## 8 — Invoke the Skill

Once a skill is active, open the interactive chat:

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

Every action — proposal creation, court review, and votes — is signed with
Ed25519 and appended to a tamper-evident Merkle chain. Verify it:

```bash
./aegisclaw audit verify
```

```
Verifying Merkle audit chain: ...
  OK: 149 entries verified
  Chain head: b161e6c7...
```

You can also explore the full audit log interactively:

```bash
./aegisclaw audit explorer
```

## 10 — Emergency Controls

If a skill misbehaves (e.g. an LLM loop that keeps invoking tools, or a tool
that processes too much data), AegisClaw provides two immediate circuit
breakers that do **not** depend on the LLM:

### Safe Mode — stop all skills, stay in chat

In the chat TUI:

```
> /safe-mode
```

This instantly deactivates every running skill and blocks new skill
activation or invocation. The chat remains open so you can investigate.
To resume normal operation:

```
> /safe-mode off
```

### Emergency Shutdown — stop everything and exit

```
> /shutdown
```

This deactivates all skills, sends a shutdown signal to the daemon, and exits
the chat. Use this when you need a full stop.

### Starting in safe mode

If you killed the daemon and want to restart without the problematic skill
reactivating:

```bash
sudo ./aegisclaw start --safe-mode
```

The daemon will start with safe mode pre-enabled. No skills will be
activated until you explicitly run `/safe-mode off` from the chat TUI
(or restart without `--safe-mode`).

## 11 — Clean Up

Stop the kernel daemon:

```bash
# Stop the kernel (Ctrl+C in the kernel terminal, or kill the background process)
sudo pkill -f "aegisclaw start"
```

## What Just Happened?

Here is the lifecycle you just walked through (steps 1–5 are fully
functional; steps 6–8 are planned):

```
propose skill ──► submit ──► court review ──► [build] ──► [activate] ──► [invoke]
     │                           │                │            │             │
   wizard            5 AI personas          codegen +       microVM       vsock
   (TUI)             cross-verified         test loop       spun up        IPC
                     via Ollama             (planned)       (planned)    (planned)
```

1. **Propose** — The interactive wizard (or CLI flags) collected your skill
   definition and created a governed proposal with a `SkillSpec`.
2. **Submit** — Transitioned the proposal from `draft` to `submitted`.
3. **Court Review** — Five LLM personas (CISO, Senior Coder, Security
   Architect, Tester, User Advocate) each reviewed the proposal independently,
   cross-verified across multiple models, and voted.
4. **Human Vote** — If the court escalated (common with small models), a
   human operator cast the deciding vote.
5. **Audit** — Every step was cryptographically signed and appended to the
   Merkle audit chain, verifiable with `aegisclaw audit verify`.

Every action is cryptographically signed and appended to the Merkle audit chain.

## Next Steps

- **Add network access** — Try a skill that calls an external API (the wizard
  will prompt you for allowed hosts and ports).
- **Use secrets** — Store API tokens with `./aegisclaw secret add` and
  reference them in a new skill proposal.
- **Explore the TUI dashboard** — Run `./aegisclaw status --tui` for a live
  view of sandboxes, skills, and the audit log.
- **Read the architecture docs** — See `docs/architecture.md` for deep dives
  on the security model, IPC design, and governance engine.
