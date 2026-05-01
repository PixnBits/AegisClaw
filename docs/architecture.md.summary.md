# `docs/architecture.md` — Summary

## Purpose

The authoritative **Component Interaction Model** for AegisClaw. Describes the north-star architecture that all code must converge to — every component boundary is a security boundary. Status: north-star (deviations tracked in `docs/prd-deviations.md`). Last updated: 2026-04-03.

## Core Guiding Principle

> If it ever touches untrusted input (user text, LLM output, external network data, or generated code), it runs in a Firecracker microVM. No exceptions.

Only two components run on the host: the **daemon** (`aegisclaw start`) and the **CLI** (`aegisclaw chat`, etc.).

## Component Map

```
Host (root)
└── Daemon (thin coordinator: VM lifecycle, Unix socket API, proposal/audit/composition stores)
    └── microVMs (each: read-only rootfs, cap-drop ALL)
        ├── AegisHub VM (sole IPC router: MessageHub + Router + ACL + IdentityRegistry)
        │   └── vsock (all inter-VM traffic)
        ├── Agent VM (guest-agent: ReAct loop)
        ├── Court VMs ×5 (guest-agent: reviewer personas)
        ├── Builder VM (guest-agent: code generation pipeline)
        └── Skill VMs (guest-agent: skill execution)
CLI (unprivileged) ──Unix socket──► Daemon ──► AegisHub VM
```

## Key Sections

- **§3** Natural language chat flow — daemon forwards to agent VM; agent runs full ReAct loop; tool calls route via AegisHub.
- **§7** Builder pipeline — Court approval → builder VM → security gates → git commit → composition update.
- **§11** AegisHub ACL — every message checked before delivery; no direct VM-to-VM paths.
- **§13** CLI command specifications mapped to daemon API endpoints.

## Fit in the Broader System

Every package in `internal/` is designed to satisfy the invariants stated here. Deviations from this model are explicitly tracked in `docs/prd-deviations.md`.
