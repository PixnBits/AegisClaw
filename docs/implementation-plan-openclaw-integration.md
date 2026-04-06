### **Implementation Plan: OpenClaw-Inspired Features in AegisClaw**  
**Project:** AegisClaw (`clawful` branch)  
**Goal:** Integrate usability, integrations, and flexibility from OpenClaw while preserving AegisClaw’s zero-trust security model (Governance Court, audit logging, sandboxed execution).  
**Key Decision:** Keep Firecracker microVMs as the primary isolation backend; Docker sandboxing is not available on the target Linux environment and is therefore omitted from this plan.
**Reference Documents:**  
- PRD Addendum: Enhancing AegisClaw with OpenClaw-Inspired Features  
- Architecture Addendum: Hybrid Architecture Integration  

**Date:** April 5, 2026  
**Author:** Grok (with input from repo analysis)

#### **Current State Summary (for Context)**
- **AegisClaw (`clawful`)**: Go-based. Core in `cmd/aegisclaw` (host CLI/daemon), `cmd/guest-agent` (runs inside microVMs), `internal/` (builder, court, sandbox, audit, composition). Skills are generated from natural language, reviewed by the 5-person Governance Court, built with security gates, and executed via the new sandboxed script runner inside Firecracker. Dashboard at `:7878` has live tool-call UX. All components are strictly isolated.
- **OpenClaw**: Node.js/TS with central Gateway (`ws://127.0.0.1:18789`), Pi RPC runtime, workspace prompt files (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`), optional Docker sandboxing (`Dockerfile.sandbox*`), multi-channel adapters, session routing tools, and ClawHub registry.
- **Foundation to Leverage**: The hardened script runner, guest-agent, Governance Court, builder pipeline, and Merkle-tree audit log.

- All new features **must**:
- Run inside a sandbox (Firecracker microVMs are the required isolation backend).
- Declare explicit capabilities.
- Pass Governance Court review + SAST/SCA/secrets gates.
- Log actions to the append-only signed audit log.
- Support automatic rollback via composition manifests.

#### **High-Level Phased Roadmap**

**Phase 1: Core Agent Flexibility (2–4 weeks)**  
Focus: Workspace prompt injection + multi-agent routing + script runner formalization.

**Phase 2: Integrations & Usability (4–8 weeks)**  
Focus: Multi-channel gateway, voice, Canvas extensions, registry bridge.

**Phase 3: Streaming & Polish (4–6 weeks)**
Focus: Full interoperability, streaming semantics, and polish while retaining Firecracker as the isolation backend.

#### **Detailed Implementation Tasks**

**Phase 1 – Workspace & Prompt System + Session Routing**

1. **Introduce Workspace Support**  
   - Add support for `~/.aegisclaw/workspace/` (mirroring OpenClaw’s `~/.openclaw/workspace/`).  
   - Allow optional files: `AGENTS.md`, `SOUL.md`, `TOOLS.md`, and per-skill `SKILL.md`.  
   - **Where to implement**: Extend the builder pipeline (`internal/builder/`) to parse these files when a skill or agent config is processed. Inject content into the guest-agent prompt context inside the sandbox.  
   - Create a governed `agent-config` skill that accepts JSON-style config (like OpenClaw’s `openclaw.json`).  
   - **Acceptance**: `aegisclaw skill add` or chat can reference workspace prompts; changes route through Court.

2. **Implement Session Routing Tools**  
   - Add governed skills: `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn`.  
   - Use sandbox-aware IPC (via existing AegisHub router) for cross-session communication.  
   - **Where**: New package `internal/skills/sessions/` or extend script runner to support these as built-in tool flows.  
   - Tie into capability declarations (e.g., “can_access_other_sessions”).

3. **Formalize Sandboxed Script Runner**  
   - Make the script runner the default execution backend for dynamic tools/scripts.  
   - Enhance capability declaration in skill manifests (network, fs, devices, etc.).  
   - **Where**: `internal/sandbox/` and `cmd/guest-agent/`. Ensure enforcement works for Firecracker.
   - Add support for safe host-device proxying (e.g., audio later).

**Phase 2 – Multi-Channel & Advanced UX**

4. **Multi-Channel Gateway**  
   - Build a thin, auditable Gateway component (inspired by OpenClaw’s central Gateway).  
   - Start with high-value channels: Telegram, Discord, WhatsApp (via official libs or bridges), Slack, Matrix.  
   - Route all traffic through approved sandbox network policies.  
   - **Where**: New `internal/gateway/` package. Host coordinator (`cmd/aegisclaw`) starts it; actual protocol handling in a dedicated sandbox. Support DM pairing/allowlists under Court oversight.  
   - Reuse patterns from OpenClaw adapters where license-compatible.

5. **Voice and Canvas Extensions**  
   - Voice wake/talk: Governed skill using proxy-injected host audio + TTS fallback.  
   - Canvas: Extend dashboard (`:7878`) into a visual workspace with agent-driven UI and tool-call visibility (build on existing live tool-call UX).  
   - **Where**: `internal/skills/voice/`, extend dashboard code, and script runner for UI-related flows.

6. **ClawHub Registry Bridge**  
   - Add read-only client skill. Imported skills auto-submit to Governance Court.  
   - **Where**: `internal/registry/` or as a skill in `internal/skills/`.

**Phase 3 – Interoperability & Docker Migration**

7. **Streaming & Agent Loop**  
   - Extend `cmd/guest-agent` to support tool/block streaming and OpenClaw-style Pi RPC semantics.  
   - This enables easier porting of OpenClaw tools.

8. **Sandbox Orchestrator (Firecracker-focused)**  
   - Abstract isolation behind a `SandboxOrchestrator` interface while retaining Firecracker as the implemented backend.  
   - Keep the code modular so alternative backends can be considered in the future if/when supported on the target platform.  
   - Add config validation and helpful doctor messages rather than a Docker mode.  
   - **Critical**: Maintain read-only rootfs, capability dropping, secrets proxy, and network policy enforcement for Firecracker.  
   - **Where**: `internal/sandbox/orchestrator.go`; update `cmd/aegisclaw` and composition logic to use the orchestrator abstraction.

9. **Onboarding & Polish**  
   - Implement `aegisclaw onboard` and `aegisclaw doctor`.  
   - Add comprehensive docs and help text.

#### **Technical Guidelines for Implementation**

- **Security First**: Every new file/package must enforce sandboxing. Use existing Court + gates. Never bypass audit logging.
- **Go Best Practices**: Keep code modular (`internal/` packages). Use interfaces for pluggable components (e.g., isolation backend).
- **Testing**: Add unit + integration tests for new skills, sandbox transitions, and end-to-end flows. Use audit log verification.
- **Configuration**: Extend existing `config/` with workspace paths and isolation mode.
- **Dependencies**: Minimize new external deps. Prefer reusing AegisHub IPC and guest-agent.
- **Migration Safety**: Keep existing Firecracker workflows working. Do not introduce a Docker-backed default or deprecation for Firecracker.
- **Inspired by OpenClaw** (but adapt securely):
   - Prompt injection model
   - Session tools
   - (No Docker sandbox patterns are used in this plan)
   - Multi-channel design

#### **Success Criteria (Overall)**
- Users can run a multi-channel assistant with voice/Canvas using governed skills and workspace prompts.
- All features maintain or exceed current security (verified via audit logs and tests).
- Firecracker microVMs remain the supported and default isolation backend on Linux.
- Onboarding feels as smooth as OpenClaw’s `openclaw onboard`.
- Backward compatibility for existing skills and workflows.

#### **Recommended Development Workflow**
1. Start with Phase 1 in a feature branch (e.g., `feature/openclaw-hybrid`).
2. Implement one small piece at a time (e.g., workspace parsing first).
3. Run full Governance Court flow on every change.
4. Test in the Firecracker environment.
5. Update docs and the two addendums as you go.
6. Use Copilot with this plan + the referenced addendums open.

**Next Steps Suggestion**: Begin by exploring `internal/builder/` and `cmd/guest-agent/` to plan prompt injection. Then prototype the workspace directory handling as a new governed skill.
