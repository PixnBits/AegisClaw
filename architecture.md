### Architecture Specifications – Secure Implementation

**High-Level Components** (all on Linux)  
- **Immutable Kernel** (Go binary, signed at build): Bootstrap, orchestrates everything, mediates all IPC. Minimal TCB (like SeedClaw).  
- **Sandbox Manager**: Creates per-skill/per-review microVMs. Network proxy for secrets. Enforces read-only, cap-drop, cgroup limits, private network.  
  - Current: FirecrackerRuntime (firecracker-go-sdk + firecracker-containerd)
  - Future: DockerSandboxRuntime (auto-detected when `docker sandbox` CLI supports Linux microVMs)  
  - All sandboxes: private vsock/TCP IPC only, network proxy for secrets, read-only FS except /workspace, cap-drop ALL, seccomp, cgroup v2 limits.  
  - Skill activation = "firecracker run skill-xxx" (or equivalent) → kernel mediates start/stop.
- **Message-Hub** (core skill): Only IPC router (JSON-over-vsock/TCP). No direct skill-to-skill.  
- **Governance Court Engine**: Orchestrates isolated reviewer LLMs (different personas/models). Uses structured JSON schemas + cross-verification.  
- **Skill Builder**: Dedicated builder sandbox – generates code, commits to local git repo (`./skills/`), creates PR branch, runs tests.  
- **Secret Vault**: Kernel-managed (SOPS/age or simple encrypted store). Decrypts only for target skill microVM at launch; injected via proxy or tmpfs mount.  
- **Ollama Router**: Local Ollama instance (models in read-only mount to Ollama skill only). Ensemble routing per role.  
- **Audit & Rollback Store**: Merkle-tree + snapshots (tar of state + git tags).  

**Data Flow for Skill Addition** (Slack example)  
User → Kernel → Proposal Wizard → Court (reviewers in sandboxes) → Builder Sandbox (git) → Review Iteration → Final Approval → Kernel applies Docker Sandbox update → Activate skill microVM.

**Security Mitigations**  
- API keys: Proxy injection only – never in prompt, code, or filesystem visible to agent/LLM.  
- Antagonistic skills: Impossible (network default deny, no host mounts, separate microVM kernel).  
- Prompt injection / model backdoors: Mitigated by multi-model Court + small audited models + sandboxed reviewers.  
- Container escape: MicroVM hardware boundary + seccomp.  
- Audit tampering: Append-only Merkle + kernel-only writes.

**Tech Stack** (Linux-only, minimal deps)  
- Kernel: Go (static binary).  
- Runtime: Docker Sandboxes (microVMs) + Firecracker option.  
- VCS: Git (local repo).  
- LLM: Ollama + ensemble models.  
- Secrets: SOPS/age.  
- IPC: vsock or loopback TCP with mTLS.  

**Pluggable & Extensible**  
- Sandbox backends interchangeable via config.  
- New personas added by Court config + prompt templates (git-managed).  
- Self-host cycle: AegisClaw repo itself lives in `./self/`; changes follow identical pipeline.
