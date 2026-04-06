### **Addendum: Enhancing AegisClaw with OpenClaw-Inspired Usability, Integrations, and Agent Flexibility**  
**Version:** 1.1 (updated April 2026 — Firecracker-only per implementation decision)  
**Date:** April 5, 2026  
**Author:** Grok (based on comparative analysis of OpenClaw and AegisClaw feature/script-runner branch)  
**Purpose:** Define requirements for AegisClaw to adopt the broad usability, multi-channel support, prompt-driven flexibility, and ecosystem benefits of OpenClaw—while preserving (and strengthening) AegisClaw’s core security, auditability, and zero-trust model.

#### **Background and Strategic Rationale**
AegisClaw and OpenClaw are complementary local-first AI agent runtimes. OpenClaw excels in usability (20+ messaging channels, voice modes, Canvas UI, declarative workspaces), flexible prompt injection, mature multi-agent routing, and a rich tool/skill ecosystem. AegisClaw provides superior isolation (Firecracker microVMs), governed skill generation, mandatory AI review (Governance Court), security gates, and verifiable audit logging.

**Key Alignment Decision:**  
Direct Firecracker usage for skills and components will be migrated to **Docker Sandboxes** once robust Linux support is implemented. *Note (April 2026): Docker sandbox support is not available on Linux at this time; this migration is deferred indefinitely. Firecracker remains the only supported isolation backend.* This maintains strong isolation (building on OpenClaw’s existing Docker sandbox model for non-main sessions) while simplifying cross-platform support, reducing KVM dependency, and enabling easier adoption of containerized tool execution. Firecracker may remain as an optional high-security mode.

The goal is **hybrid excellence**: AegisClaw gains OpenClaw’s user-facing breadth and developer experience **without weakening** its paranoid-by-design security invariants. New features must run as governed skills or components inside sandboxes (Docker or Firecracker), declare explicit capabilities, pass the Governance Court + security gates, and contribute to the append-only Merkle-tree audit log.

#### **High-Level Objectives**
- Achieve functional parity with OpenClaw’s usability and integrations.
- Leverage the new **sandboxed script runner** (in `feature/script-runner`) as the execution backbone for dynamic tools and scripts.
- Support declarative prompt injection (workspace-style) alongside governed code generation.
- Ensure all enhancements are auditable, versioned, and reversible.
- Maintain zero cloud dependency and full local execution.

#### **Functional Requirements**

**1. Workspace and Prompt-Driven Agent Configuration (Phase 1)**
- Extend the skill system to support optional workspace directories (e.g., `~/.aegisclaw/workspace/`) containing `AGENTS.md`, `SOUL.md`, `TOOLS.md`, and per-skill `SKILL.md` files.
- The builder pipeline (running in a sandbox) shall parse and inject these prompts into the guest-agent context at runtime.
- Provide a governed `agent-config` skill that allows JSON-style configuration (inspired by OpenClaw’s `openclaw.json`) as input to the Governance Court. Changes must be reviewed, versioned, and signed before activation.
- Success criteria: Users can customize agent personality and tools declaratively without always writing code.

**2. Multi-Agent and Session Routing (Phase 1)**
- Implement OpenClaw-style tools as first-class governed skills: `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn`.
- Enable per-workspace/per-channel agent isolation and routing via sandboxed IPC (proxied securely between Docker/Firecracker instances).
- Support session-aware tool allow/denylists that integrate with the existing capability declaration system.

**3. Sandboxed Script Runner as Execution Core (Phase 1–2)**
- Formalize the existing script runner as the backend for any dynamic execution (scripts, tool flows, browser actions, cron, etc.).
- Skills/tools must declare required capabilities (network, filesystem, host device access, etc.) in their manifest. These are reviewed by the Governance Court and enforced at the sandbox level (Docker seccomp/AppArmor or Firecracker equivalents: read-only rootfs, `cap-drop ALL`, secrets proxy).
- Add support for safe host-device proxying (e.g., audio for voice, browser CDP) while keeping the agent logic isolated.

**4. Multi-Channel Gateway and Integrations (Phase 2)**
- Build a thin, auditable host-side (or sandboxed) Gateway component that handles WebSocket and messaging protocols.
- Support at least the following channels initially (prioritized by value): Telegram, Discord, WhatsApp (via official APIs or bridges), Slack, Matrix, iMessage.
- All external traffic routes through approved sandbox network policies. No direct host exposure.
- Mirror OpenClaw’s DM policy options (pairing/allowlist) with Governance Court oversight for changes.

**5. Voice, Canvas UI, and Advanced Tools (Phase 2)**
- Add voice wake/talk as a governed skill, using proxy-injected host audio devices and TTS fallback.
- Extend the existing live dashboard (port 7878) into a full Canvas-like visual workspace with agent-driven UI elements and tool-call visibility.
- Port high-value OpenClaw tools (browser control via CDP, cron, nodes for local actions) as sandboxed skills with explicit capability declarations.

**6. Skill Registry and Ecosystem Bridge (Phase 2)**
- Add a read-only ClawHub-compatible registry client as a governed skill.
- Any imported skill is automatically submitted to the Governance Court for full review (5-AI reviewers + SAST/SCA/secrets gates) before activation.
- Maintain AegisClaw’s versioned, signed deployment and automatic rollback on health failure.

**7. Streaming and Agent Loop Semantics (Phase 3)**
- Extend the `guest-agent` binary to support tool/block streaming and OpenClaw-style agent-loop RPC semantics.
- This enables seamless migration or interoperability of existing OpenClaw tools/skills.

#### **Non-Functional Requirements**
- **Security Invariants (Mandatory):** Every new feature/component (gateway, prompt injector, channel handler, UI extension) must:
  - Execute inside a Docker sandbox (preferred on Linux) or Firecracker microVM.
  - Declare capabilities explicitly and pass Governance Court + security gates.
  - Log all actions to the append-only Ed25519-signed Merkle-tree audit log.
  - Support automatic rollback on failure or policy violation.
- **Migration Path:** Replace direct Firecracker calls with Docker sandbox orchestration once Linux Docker sandbox support is mature. Provide a configuration flag to choose isolation backend (Docker vs. Firecracker high-security mode).
- **Cross-Platform Support:** Prioritize Linux with Docker; provide graceful degradation or alternatives for other OSes during transition.
- **Usability and Onboarding:** Implement `aegisclaw onboard` and `aegisclaw doctor` commands mirroring OpenClaw’s experience. Include comprehensive docs for new features.
- **Performance and Latency:** Governance Court review should not block rapid iteration for trusted/dev workflows (optional fast-path with audit warnings). Sandbox overhead must remain acceptable for interactive use.
- **Auditability and Traceability:** All enhancements must contribute verifiable entries to the audit log. Add queries like `audit why <feature>` for new capabilities.
- **Backward Compatibility:** Existing AegisClaw skills, Court workflows, guest-agent, and dashboard remain unchanged and fully functional.
- **Testing:** Add regression test suites covering script runner, new integrations, sandbox migration, and end-to-end audit verification.

#### **Phased Implementation Roadmap**
- **Phase 1 (2–4 weeks):** Workspace/prompt injection, multi-agent routing, script-runner formalization.
- **Phase 2 (4–8 weeks):** Multi-channel gateway, voice/Canvas extensions, registry bridge.
- **Phase 3 (4–6 weeks):** Streaming support, cross-platform polish, Docker migration completion.
- **Ongoing:** Security hardening, documentation, and ClawHub interoperability testing.

#### **Success Metrics**
- User can configure and run a multi-channel personal assistant with voice and visual Canvas using governed skills.
- All features maintain or exceed current AegisClaw security posture (verified via audit logs and penetration-style testing).
- Adoption of imported OpenClaw-style skills increases without increasing attack surface.
- Onboarding time for new users approaches OpenClaw’s `openclaw onboard` experience.

#### **Risks and Mitigations**
- **Latency from Governance Court:** Mitigation — Optional dev-mode bypass with full logging.
- **Docker Sandbox Maturity on Linux:** Mitigation — Keep Firecracker as fallback; prioritize sandbox feature development.
- **Integration Complexity:** Mitigation — Start with high-value channels and reuse OpenClaw SDK patterns where license-compatible.

This addendum should be reviewed by the core team and attached to the main PRD. It provides a concrete, security-first path to combine the best of both projects.
