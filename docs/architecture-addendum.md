### **Addendum: Hybrid Architecture Integration with OpenClaw-Inspired Features**  
**Version:** 1.0  
**Date:** April 5, 2026  
**Author:** Grok (based on analysis of OpenClaw and AegisClaw `feature/script-runner` branch)  
**Purpose:** Define the target architecture for AegisClaw to incorporate OpenClaw’s usability, multi-channel integrations, declarative workspaces, and agent flexibility while preserving AegisClaw’s paranoid-by-design security model. This addendum formalizes the migration path from direct Firecracker microVMs to Docker Sandboxes on Linux.

#### **Strategic Architectural Vision**
AegisClaw’s current architecture is centered on strict zero-trust isolation using Firecracker microVMs for every component (main agent, Governance Court reviewers, builder pipeline, and skills), mandatory AI governance, signed Merkle-tree audit logging, and capability-based execution. OpenClaw’s architecture, in contrast, uses a central **Gateway** control plane with WebSocket-based routing, the **Pi RPC runtime** for agent execution, workspace-based prompt injection, and optional Docker sandboxing for non-main sessions.

The hybrid target architecture will:
- Retain AegisClaw’s Governance Court, builder pipeline, audit log, and guest-agent as core security primitives.
- Adopt OpenClaw-style usability layers (workspaces, multi-channel gateway, session routing, Canvas-like UI) as governed, sandboxed skills or components.
- Migrate direct Firecracker usage to **Docker Sandboxes** on Linux (once mature), using Docker as the primary isolation backend for better cross-platform compatibility and reduced KVM dependency. Firecracker will remain available as an optional high-assurance mode.
- Leverage the hardened **sandboxed script runner** (from `feature/script-runner`) as the unified execution engine for dynamic tools, scripts, and channel handlers.

This results in a **layered, pluggable architecture**:
- **Security Core Layer** (AegisClaw-native): Governance Court, builder, audit, composition manifests.
- **Execution Layer** (Hybrid): Docker/Firecracker sandboxes + script runner + guest-agent.
- **Usability & Integration Layer** (OpenClaw-inspired): Workspace prompt injection, multi-channel gateway, session tools, voice/Canvas extensions.

All new or imported components must declare capabilities, pass Governance Court review + security gates (SAST/SCA/secrets/policy), execute inside a sandbox, and log actions verifiably.

#### **Key Architectural Similarities and Differences**
| Aspect                    | Current AegisClaw (Firecracker-based)                  | OpenClaw                                      | Target Hybrid Architecture                          |
|---------------------------|-------------------------------------------------------|-----------------------------------------------|----------------------------------------------------|
| **Isolation**            | Mandatory per-component Firecracker microVMs (read-only rootfs, cap-drop ALL, network only if approved) | Optional Docker for non-main sessions; host for main | Docker Sandboxes (primary on Linux) or Firecracker (high-security mode); unified via script runner |
| **Control Plane**        | Host coordinator + AegisHub IPC router               | Central Gateway (WebSocket ws://127.0.0.1:18789) | Thin auditable Gateway skill/component (sandboxed or proxied) |
| **Agent Execution**      | Guest-agent binary in each VM; script runner         | Pi RPC runtime with tool/block streaming     | Extended guest-agent + script runner + Pi-compatible streaming semantics |
| **Skill Definition**     | Natural language → governed code generation          | Declarative workspaces (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`) | Both: Prompt injection supported + governed generation |
| **Multi-Agent**          | Per-skill VM isolation                               | Sessions & workspaces with routing tools     | Sandboxed session routing tools (`sessions_*`)    |
| **Auditability**         | Append-only Ed25519-signed Merkle-tree log           | Basic usage tracking                         | Enhanced with full traceability for all new features |
| **Integrations**         | Limited (TUI + dashboard)                            | 20+ messaging channels, voice, nodes, browser | Governed multi-channel gateway + proxying         |

#### **Target Component Model (Updated)**
1. **Host Coordinator / CLI-TUI**  
   - Remains the user entrypoint (`aegisclaw` commands, chat, dashboard at :7878).  
   - Delegates all substantive work to sandboxed components.  
   - New: `aegisclaw onboard` and `doctor` for simplified setup of channels and workspaces.

2. **Security & Governance Core** (Unchanged)  
   - **Governance Court** (5 AI reviewers in isolated sandboxes).  
   - **Builder Pipeline** (sandboxed code generation + mandatory gates).  
   - **Audit Log** (Merkle-tree, Ed25519-signed; extended queries for new features, e.g., `audit why channel:telegram`).  
   - **Composition Manifests** (versioned, signed deployments with automatic rollback).

3. **Isolation & Execution Layer**  
   - **Sandbox Orchestrator**: Abstracts Docker (preferred on Linux) and Firecracker. Configurable via manifest (`isolation: docker | firecracker`).  
   - **Sandbox Specification**: Read-only base image, ephemeral writable layers, capability enforcement (seccomp/AppArmor for Docker; equivalent for Firecracker), secrets proxy, network policy enforcement.  
   - **Script Runner**: Hardened execution backend for all dynamic logic (scripts, tool flows, channel handlers, browser actions). Supports capability declarations and live tool-call UX.  
   - **Guest-Agent**: Extended to support OpenClaw-style RPC streaming, block streaming, and agent-loop semantics for better interoperability.

4. **Usability & Integration Layer (New/Extended)**  
   - **Workspace System**: Optional `~/.aegisclaw/workspace/` directory with prompt files (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, per-skill `SKILL.md`). Builder parses and injects at runtime inside the sandbox.  
   - **Multi-Channel Gateway**: Thin, sandboxed or proxied component handling WebSocket and messaging protocols (Telegram, Discord, WhatsApp, etc.). Routes traffic through approved network policies. Supports DM policies and allowlists under Court oversight.  
   - **Session & Routing Tools**: Governed skills implementing `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn` with sandbox-aware IPC.  
   - **Voice & Canvas**: Governed skills using proxy-injected host devices (audio) and extended dashboard for visual agent-driven UI.  
   - **Skill Registry Bridge**: Read-only ClawHub client; imported skills auto-routed through full governance process.

5. **Data Flow (High-Level)**  
   User Input (CLI/TUI/Channel) → Host Coordinator → Sandboxed Gateway (if channel) → Session Router → Sandboxed Guest-Agent (with prompt injection) → Script Runner / Tool Execution (capability-checked) → Results streamed back → Audit Log → Response to Channel/UI.

#### **Non-Functional Architectural Requirements**
- **Security Invariants**: No component bypasses the sandbox, Governance Court, or audit log. All external I/O (network, devices) must be explicitly declared and approved.
- **Migration Strategy**: 
  - Phase out direct Firecracker orchestration in favor of Docker sandbox primitives.
  - Maintain dual-mode support during transition (`--isolation=firecracker` flag for high-security users).
  - Reuse patterns from OpenClaw’s `Dockerfile.sandbox` where compatible.
- **Performance**: Sandbox overhead minimized for interactive use; optional fast-path dev mode with enhanced logging.
- **Extensibility**: New features added as governed skills or pluggable composition modules.
- **Observability**: Unified logging and audit trails across Docker/Firecracker backends.
- **Backward Compatibility**: Existing Firecracker-based skills and workflows continue to function (with deprecation warnings for direct Firecracker usage).

#### **Phased Architectural Evolution**
- **Phase 1**: Introduce workspace prompt injection, session routing tools, and script-runner formalization within current sandbox model.
- **Phase 2**: Implement sandbox orchestrator abstraction and multi-channel gateway; begin Docker integration.
- **Phase 3**: Complete Docker migration on Linux, extend guest-agent for streaming, add voice/Canvas, and finalize registry bridge.
- **Ongoing**: Monitor sandbox performance; maintain Firecracker as opt-in for maximum isolation.

#### **Risks and Mitigations**
- Docker sandbox maturity on Linux → Prioritize its development; keep Firecracker fallback.
- Increased attack surface from channels/integrations → Strict capability declarations + Court review + network policies.
- Complexity in dual isolation backends → Abstract behind orchestrator; comprehensive testing.

This addendum aligns the AegisClaw architecture with the functional requirements outlined in the PRD addendum, creating a secure, flexible, and user-friendly local AI agent platform that combines the strengths of both projects.
