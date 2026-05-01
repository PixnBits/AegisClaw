# `docs/architecture-addendum.md` — Summary

## Purpose

Defines the target hybrid architecture for integrating OpenClaw-inspired usability layers (multi-channel gateway, workspace injection, session routing, Canvas UI) into AegisClaw — while keeping Firecracker microVMs as the only supported isolation backend. Version 1.1 (April 2026): Docker migration deferred indefinitely; Firecracker is the required backend.

## Key Contents

### Strategic Vision
Adopts a **three-layer architecture**:
1. **Security Core Layer** (AegisClaw-native): Governance Court, builder, audit, composition manifests.
2. **Execution Layer** (hybrid): Firecracker sandboxes + script runner + guest-agent.
3. **Usability & Integration Layer** (OpenClaw-inspired): workspace injection, multi-channel gateway, session tools, voice/Canvas.

### Comparison Table
Side-by-side comparison of current AegisClaw (Firecracker, host coordinator + AegisHub), OpenClaw (WebSocket Gateway, Pi RPC runtime), and the target hybrid across: isolation, control plane, agent execution, multi-channel support, workspace, skill registry, and audit.

### Target Components
- **Sandbox Orchestrator interface** — pluggable abstraction for Firecracker (only current backend).
- **AegisHub** — retained as sole IPC router; vsock-only, no network egress.
- **Script Runner** — unified execution engine for dynamic tools, channel handlers, and skills.
- **Gateway skill** — sandboxed multi-channel adapter replacing direct host network access.

## Fit in the Broader System

Companion to `docs/PRD-addendum.md` (requirements) and `docs/implementation-plan-openclaw-integration.md` (phased delivery). Influences `internal/gateway`, `internal/sandbox`, `internal/ipc`, and the workspace customisation system.
