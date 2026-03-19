**Recommendation Update (Senior Architect view)**  
Fork **SeedClaw** directly into a new repository **`PixnBits/AegisClaw`** (MIT license).  
- SeedClaw already delivers 80 % of Epic 1 (Go kernel, message-hub IPC, audit trail, skill registry, network policies, strict mounts).  
- Port the Governance Court, proposal structs, reviewer personas, and snapshot-rollback logic from your AegisCourt `fourth-implementation_claude-opus-4.6` branch (Go, same language — 2–3 days of copy/adapt).  
- Replace the Docker orchestration layer with the FirecrackerRuntime above. This directly fixes the “start/stop does nothing, no containers appear” issue you saw and the mutation-generation problems (Court now gates every change).  

No existing OSS project (OpenClaw/NanoClaw/Moru/etc.) gives you Firecracker + full SDLC Court + secret proxy + self-improvement loop out of the box. This fork + targeted upgrades is the fastest, most secure path to a production-ready, self-hosting system.

**Bootstrap Instructions (do these 3 steps manually before feeding tasks to your code-gen LLM)**  
1. `gh repo fork PixnBits/SeedClaw PixnBits/AegisClaw --clone` (or clone + rename).  
2. `cd AegisClaw && go mod edit -module github.com/PixnBits/aegisclaw && go mod tidy`.  
3. Enable KVM, install official Firecracker + jailer binaries (verify SHA256 from firecracker-microvm.github.io), create `/var/lib/aegisclaw/rootfs-templates` (minimal Alpine + guest-agent stub).  

Then paste the tasks below one-by-one (or in small batches) to your Ollama model (e.g., qwen2.5-coder:14b + nemotron-3-nano). Each task produces **full production code**, tests, git commit message, and security comments. Once Epic 1 + 2 are done, the system can review and apply its own future changes via the Court.

### Epic 1: Core Kernel & Sandbox Orchestrator (v0.1) – Must have working Firecracker microVMs

**Task 1.1** – Project skeleton & CLI foundation  
Create full Go layout (`cmd/aegisclaw/`, `internal/kernel/`, `internal/sandbox/`, `internal/audit/`, `internal/config/`). Use cobra for CLI with subcommands: `start`, `status`, `sandbox ls/start/stop`, `version`. Load config from `~/.config/aegisclaw/config.yaml` (defaults for Firecracker paths, rootfs template, audit dir). Static binary build (`go build -ldflags="-s -w"`). Output: full files + `go.mod` update + commit “chore: AegisClaw skeleton from SeedClaw fork + cobra CLI”.

**Task 1.2** – Immutable Kernel core  
`internal/kernel/kernel.go` – singleton with Ed25519 signing key (generated/stored securely on first run). All actions routed through Kernel.SignAndLog(). Adapt SeedClaw’s `seedclaw.go` control-plane listener (now on vsock). Output: kernel singleton + signing + commit.

**Task 1.3** – Sandbox Manager interface + FirecrackerRuntime  
Define `SandboxSpec` (ID, Name, Resources, NetworkPolicy, VsockCID). Implement `SandboxManager` interface. Full `FirecrackerRuntime` using `firecracker-go-sdk`: jailer UID/GID isolation, vsock device, tap network (default deny), read-only root + /workspace overlay, cgroup v2 limits, seccomp profile. Use firecracker-containerd shim option for future OCI compatibility. Output: complete runtime + security profile + commit.

**Task 1.4** – Rootfs & Guest Agent  
Build minimal Alpine ext4 rootfs template (include busybox, Go guest-agent binary as PID 1). Guest agent listens on vsock, supports exec/tool-call, file ops in /workspace only, status reporting. Provide build script + guest-agent `cmd/guest-agent/main.go`. Output: rootfs builder + guest agent + commit.

**Task 1.5** – Sandbox lifecycle & activation  
Implement Create/Start/Stop/Delete/List/Status. Kernel exclusively owns Firecracker processes. `claw skill activate <name>` spins microVM and registers in persistent registry (JSON + Merkle hash). Reversible snapshot metadata. Adapt SeedClaw registry logic. Output: full lifecycle + CLI commands + commit.

**Task 1.6** – vsock IPC & Message-Hub  
Start core “message-hub” skill in its own microVM on kernel launch. JSON-over-vsock router with sender VM-ID validation. No direct skill-to-skill communication. Output: IPC layer + message-hub registration + commit.

**Task 1.7** – Audit & tamper-evident logging  
Adapt SeedClaw append-only log to Merkle-tree format (each entry: UUID, prev_hash, payload, signature). Every sandbox action + kernel call is signed. Output: Merkle audit package + commit.

**Task 1.8** – Hardening, tests & first-run  
Enforce default-deny network, no host mounts, cleanup on shutdown. Write integration tests (start/stop test VM, verify isolation via logs). `claw start` command boots kernel + message-hub. Output: tests + systemd unit template + commit.

### Epic 2: Governance Court & Multi-Persona Reviewers (v0.2)

**Task 2.1** – Proposal model & storage  
`internal/proposal/` – struct + git storage (use go-git; proposals stored as branches in `./skills/.git`). JSON schema validation. Port from AegisCourt proposal schema. Output: full model + git integration + commit.

**Task 2.2** – Court Engine  
Orchestrator that, for any proposal, spins isolated reviewer microVMs (one per persona). Aggregate scores, risk heatmap. Output: engine + commit.

**Task 2.3** – Persona system  
`/config/personas/` YAML files: CISO, SeniorCoder, SecurityArchitect, Tester, UserAdvocate (with system prompt + required JSON output schema: riskScore, verdict, evidence, questions). Output: templates + commit.

**Task 2.4** – Reviewer execution  
For each persona: launch Firecracker reviewer sandbox, inject proposal + persona prompt via vsock, parse structured JSON response. Cross-verify with ≥2 Ollama models. Output: reviewer loop + commit.

**Task 2.5** – Consensus & iteration  
Multi-round review until consensus or human vote required (Enterprise mode). Store reviews in proposal. Output: consensus logic + commit.

**Task 2.6** – Court CLI & human override  
`claw propose <description>`, `claw court review <id>`, `claw court vote <id> approve`. TUI summary table. Output: CLI + commit.

### Epic 3: Skill Builder Pipeline + Git Integration (v0.3)

**Task 3.1** – Builder sandbox + code-gen loop  
Dedicated builder microVM that receives refined spec and generates/iterates code. Output: builder runtime + commit.  
**Task 3.2** – Git workflow (commit + branch)  
Use go-git to create PR-style branch, commit changes, generate diff. Output: git pipeline + commit.  
**Task 3.3** – Code review iteration in Court  
Feed diffs to reviewers (Coder + CISO) for approval loop. Output: review integration + commit.  
**Task 3.4** – Build & test artifact  
Inside builder sandbox: `go build`, static analysis, signature. Output: build step + commit.

### Epic 4: Secret Proxy & Network Policy Enforcement (v0.4)

**Task 4.1** – Secret Vault (age/SOPS)  
Kernel-managed encrypted store. Output: vault + commit.  
**Task 4.2** – vsock-based secret proxy  
Inject credentials at runtime into skill VM via guest-agent (never in prompt or FS). Output: proxy + commit.  
**Task 4.3** – Network policy enforcement  
Tap device + kernel proxy; enforce skill metadata allow-list (default deny). Output: enforcement + commit.

### Epic 5: Human Interfaces (CLI + TUI) & Proposal Wizard (v0.5)

**Task 5.1** – Interactive proposal wizard  
Step-by-step refinement questions + risk sliders. Output: wizard + commit.  
**Task 5.2** – Bubbletea TUI dashboard  
Live Court progress, skill map, audit grep, rollback. Output: TUI + commit.

### Epic 6: Ollama Ensemble + Model Manager (v0.6)

**Task 6.1** – Ollama router & ensemble  
Local Ollama only; route by role (fast reviewer = Llama-3.2-3B, reasoning = Mistral-Nemo). Hash verification on model download. Output: router + commit.  
**Task 6.2** – Cross-model verification  
Run critical Court steps on 2+ models. Output: verification layer + commit.

### Epic 7: Self-Improvement Loop (v1.0)

**Task 7.1** – Self-mutation handler  
Treat AegisClaw repo itself as a skill; apply approved Court patches via git pull + rebuild. Output: self-apply logic + commit.  
**Task 7.2** – Reversible snapshot + rollback for kernel  
Full state tar + git revert on failure. Output: snapshot system + commit.  
**Task 7.3** – End-to-end self-improvement test  
Propose a small kernel patch, run full Court, apply, verify. Output: test + commit.

**Next Steps**  
Feed Task 1.1 to your Ollama coder model right now — it will produce the first working `claw` binary with Firecracker support. Once Epic 1 is complete, the system can run its own Court on the remaining tasks.  

### Epic 3: Skill Builder Pipeline + Git Integration (v0.3)

**Task 3.1** – Dedicated Builder Sandbox Runtime  
Extend `SandboxSpec` with builder-specific fields (higher CPU/RAM limits, persistent /workspace overlay). Create `internal/builder/builder.go` implementing `BuilderRuntime` that inherits from `FirecrackerRuntime`. Use an enhanced rootfs template (Alpine + `apk add go git golangci-lint staticcheck make`). Builder VM exposes vsock for receiving refined proposals and returns generated files/logs. Enforce: read-only everywhere except /workspace, network only to Ollama + git (if needed for modules). Kernel exclusively manages builder lifecycle. Add `claw builder status` CLI. Output: full builder runtime + updated rootfs build script in `scripts/` + commit “feat: dedicated Firecracker builder sandbox with full Go toolchain”.

**Task 3.2** – Skill Specification & Code Generation Service  
Define `SkillSpec` struct (name, description, tools, networkPolicy, secretsRefs, personaRequirements). Create `internal/builder/codegen.go` with `CodeGenerator` service that runs exclusively inside the builder sandbox. It receives RefinedProposal + existing code context via vsock, sends structured prompt (with full system templates from `./config/templates/`) to Ollama, iterates up to 3 rounds, and returns `map[string]string` of file paths → content. Use strict JSON schema for output. Output: SkillSpec + full code-generation service + prompt templates + commit.

**Task 3.3** – Git Repository Management Layer  
Create `internal/git/manager.go` using `github.com/go-git/go-git/v5`. Manage two repos: `./skills/` (user skills) and `./self/` (kernel). Support: init if missing, create feature branch `proposal-<id>`, signed commits (using kernel Ed25519 key), conflict detection, unified diff generation. All git ops logged to Merkle audit. Output: git management package with security wrappers + unit tests + commit “feat: signed git proposal branch management”.

**Task 3.4** – Proposal-to-Code Pipeline Orchestrator  
Implement `internal/builder/pipeline.go`. Flow: Court-approved Proposal → start BuilderSandbox → CodeGenerator produces files → GitManager creates branch + commits → return diff + metadata to Court. Support “edit existing skill” mode. Store build artifacts and hashes in proposal record. Output: end-to-end pipeline orchestrator + commit.

**Task 3.5** – Diff Generation, Static Analysis & Build Step  
Inside builder sandbox after code gen: generate colored unified diff, run `go test ./...`, `golangci-lint run`, `gosec`, `go build -buildmode=pie`. Attach results + signed artifact to proposal. Fail the proposal on high-severity issues. Output: analysis + diff + build pipeline + commit.

**Task 3.6** – Iterative Fix Loop with Court Feedback  
Extend pipeline so that if any reviewer rejects the code (Coder/CISO), structured comments are fed back into CodeGenerator for automatic fix rounds (max 3). Re-generate diff and re-present to Court. Output: iteration engine + commit.

**Task 3.7** – Artifact Signing & Packaging  
After successful build, kernel signs binary + manifest (Ed25519). Store in `./artifacts/<skill-id>/` with SHA256. Prepare sandbox-ready manifest (for future activation). Output: signing + packaging layer + commit.

### Epic 4: Secret Proxy & Network Policy Enforcement (v0.4)

**Task 4.1** – Kernel-Managed Secret Vault  
Implement `internal/vault/vault.go` using `filippo.io/age`. Encrypted store at `~/.config/aegisclaw/secrets/`. Kernel-only access via age identity (Ed25519-derived). CLI commands: `claw secret add <name> --skill <id>`, `claw secret list`. All access logged to Merkle audit. Output: full vault + CLI integration + commit “feat: age-based kernel secret vault”.

**Task 4.2** – Skill Secret & Network-Policy Metadata  
Extend `SkillSpec`, `SandboxSpec` and Proposal structs with `secrets: []string` (names only) and `networkPolicy: NetworkPolicy` struct (allowedHosts, ports, protocols, defaultDeny). Update all JSON schemas and validation. Never store actual secret values in git or proposals. Output: metadata extensions + validation + commit.

**Task 4.3** – vsock Secret Proxy Service  
Create lightweight proxy binary (runs inside every skill microVM). On skill launch, kernel decrypts required secrets and streams them over private authenticated vsock channel to guest-agent. Guest-agent places values in ephemeral tmpfs `/run/secrets/` (never persisted). Update guest-agent accordingly. Output: proxy + guest-agent integration + commit.

**Task 4.4** – Network Policy Engine & Parser  
Define strict `NetworkPolicy` struct. Implement parser that converts it into nftables rules (or userspace proxy inside VM). Default = DROP all outbound. Output: policy model + parser + commit.

**Task 4.5** – Firecracker Network Enforcement  
During `FirecrackerRuntime.Create/Start`: allocate per-VM tap, apply host-side nftables rules (per-skill chain) using the parsed policy. Kernel owns all rule creation/cleanup. Log every connection attempt (allowed or blocked) to audit. Output: full enforcement layer + commit.

**Task 4.6** – Secret & Network Security Tests  
Write integration tests verifying: secrets never appear in logs/prompts/disk, network blocked except allowed hosts, proxy injection never touches builder/Court sandboxes. Output: comprehensive tests + commit.

### Epic 5: Human Interfaces (CLI + TUI) & Proposal Wizard (v0.5)

**Task 5.1** – Interactive Proposal Wizard  
Implement `internal/wizard/wizard.go` using `github.com/charmbracelet/huh` (or survey). Flow: skill goal → 5–8 clarification questions → risk sliders → required personas → generate initial Proposal JSON + draft. Command: `claw propose skill "Slack API"`. Output: full wizard + commit.

**Task 5.2** – Bubbletea TUI Foundation  
Add dependencies `github.com/charmbracelet/bubbletea`, `lipgloss`, `bubbles`. Create `internal/tui/` with reusable components (table, spinner, modal, diff viewer). Output: TUI framework + commit “feat: bubbletea TUI base”.

**Task 5.3** – Court Review Dashboard TUI  
Build full interactive screen (`claw court dashboard`): live table of proposals, per-persona status/risk/evidence, keyboard navigation, view diffs, vote controls (approve/reject/ask). Real-time updates via kernel events. Output: court TUI + commit.

**Task 5.4** – Skill Status & Monitoring Dashboard  
Enhanced `claw status` TUI showing: running microVMs table, CPU/RAM per skill, isolation status, last audit entries, quick start/stop. Include log tail. Output: monitoring dashboard + commit.

**Task 5.5** – Audit Explorer & Rollback Interface  
`claw audit` and `claw rollback <id>` TUI with search/grep, Merkle chain verification, diff preview, confirmation modal. Output: explorer + rollback UI + commit.

**Task 5.6** – Main Chat / ReAct Interface  
Implement `claw chat` with persistent context, tool calling, and seamless integration with wizard/Court. Output: main ReAct loop + commit.

### Epic 6: Ollama Ensemble + Model Manager (v0.6)

**Task 6.1** – Ollama Client & Model Registry  
Create `internal/llm/ollama.go` wrapper (local-only, default localhost:11434). Maintain `ModelRegistry` in config with name, SHA256 hash, persona suitability tags. Output: client + registry + commit.

**Task 6.2** – Secure Model Manager & Verifier  
Implement `claw model list/verify/update`. Download + SHA256 check against hardcoded known-good list. Store models in read-only path shared only with reviewer sandboxes. Output: manager + verifier + commit.

**Task 6.3** – Persona-to-Model Router  
YAML config (`config/personas.yaml`) mapping persona → model(s) + temperature + JSON schema. Support fallback chain and ensemble mode. Output: routing layer + commit.

**Task 6.4** – Structured Output Enforcement  
Wrap all LLM calls with JSON schema validation + retry (max 3, temp adjustment). Parse and enforce every Court and code-gen response. Output: enforcement layer + commit.

**Task 6.5** – Cross-Model Verification Engine  
For CISO-level and kernel-impacting decisions: run on primary + secondary model, require consensus threshold (or escalate to human). Log discrepancies to audit. Output: verification engine + commit.

**Task 6.6** – Ollama Isolation Enforcement  
Ensure every Ollama call happens inside a dedicated reviewer or builder sandbox — never from kernel. Output: isolation rules + commit.

### Epic 7: Self-Improvement Loop (v1.0)

**Task 7.1** – Register AegisClaw as Self-Skill  
Create special “kernel” skill entry pointing to `./self/` repo. Extend proposal system to support self-mutations with extra validation gates. Output: self-skill registration + commit.

**Task 7.2** – Self-Proposal Handler  
Special handler for proposals targeting the kernel codebase. Routes to higher-consensus Court (CISO mandatory) and uses special builder rules. Output: self-proposal handler + commit.

**Task 7.3** – Safe Patch Application Engine  
After Court approval: git apply/patch → rebuild binary → signature verify → prepare new kernel binary. Output: patch engine + commit.

**Task 7.4** – Full State Snapshot System  
Before any self-mutation: create tar snapshot (config, registry, git refs, Merkle root, VM states). Use Firecracker snapshot where possible. Store with proposal ID. Output: snapshot engine + commit.

**Task 7.5** – Rollback & Recovery Mechanisms  
Implement `claw rollback <id>`: restore snapshot + git revert + restart kernel from previous good binary. Automatic rollback on startup failure. Output: rollback system + commit.

**Task 7.6** – Atomic Kernel Rebuild & Restart  
After successful self-build: stage new binary, graceful shutdown of all sandboxes, atomic exec of new binary (or systemd restart). Zero-downtime where possible. Output: rebuild + restart logic + commit.

**Task 7.7** – End-to-End Self-Improvement Test Suite  
Integration test that: proposes a trivial kernel change → full Court + builder + review → applies → verifies new behavior → rollback test. Output: complete test suite + commit “test: end-to-end self-improvement validation”.

**Next Steps (after you finish Epic 2)**  
1. Execute tasks in order 3.1 → 3.7, then 4.1 → 7.7.  
2. After every task: `git commit -m "$(cat commit-msg.txt)"` then run `claw court review` (once live).  
3. The system will now be able to propose and apply its own improvements.
