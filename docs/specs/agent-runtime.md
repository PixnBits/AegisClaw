# Agent Runtime VM Specification

## Overview
The Agent Runtime VM is the execution environment for autonomous agents. It is **stateless** — all persistent state lives in the Memory VM or Store VM. Each agent runs in its own isolated Firecracker microVM.

## Responsibilities
- Execute the 6-step Agent Loop: Observe → Think → Plan → Act → Execute → Judge
- Handle interleaved user messages and background/proactive tasks
- Call skills/tools exclusively through AegisHub
- Maintain conversation context in Memory VM
- Support both interactive and autonomous operation modes

## Security & Isolation (Paranoid Model)
- Assume all inputs from other components are potentially hostile
- All skill/tool execution is sandboxed
- No direct network, disk, or host access — everything routes through AegisHub
- No long-term secrets held in the VM
- Mandatory code signing verification for loaded skills

## Communication
- **Only** allowed interfaces:
  - vsock / JSON-RPC to AegisHub (for skills, memory access, notifications)
  - vsock to Memory VM (short-term context)
- All outbound actions (network, git, etc.) must go through Network Boundary via AegisHub
- Court Scribe notifications for governance events

## Key Interfaces
- `agent.loop.step(...)`
- `agent.memory.*` (via Memory VM)
- `skill.invoke(...)` → AegisHub
- Event subscription for user messages and court feedback

## Traceability

**Driven by:**
- [../prd/runtime-architecture.md](../prd/runtime-architecture.md) — Agent Runtime section
- [../prd/security-model.md](../prd/security-model.md) — Isolation and zero-trust rules
- [../prd/governance-court.md](../prd/governance-court.md) — Court feedback loops
- [../architecture.md](../architecture.md) — Overall system shape

**See also:**
- [../specs/aegishub.md](../specs/aegishub.md)
- [../specs/memory-vm.md](../specs/memory-vm.md)
- [../specs/network-boundary.md](../specs/network-boundary.md)
- [../specs/court-scribe.md](../specs/court-scribe.md)

## Non-Goals
- Persistent storage
- Direct secret handling
- Long-lived processes beyond a single agent session (ephemeral per agent)