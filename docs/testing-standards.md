# Testing Standards for AegisClaw

**Status**: Authoritative standard for quality assurance on this project.

This document defines the expected testing approach for AegisClaw. It is designed both for human contributors and for LLM agents (Grok Build, Cursor, etc.) working on the codebase.

## Core Principles

1. **Layered Testing** — Fast feedback at lower layers; deeper validation at higher layers.
2. **Startup & Lifecycle as First-Class Citizens** — Many defects manifest during daemon + microVM startup. Tests must explicitly validate healthy startup, component registration, and pre-warm behavior.
3. **Self-Documenting Tests** — Because this is a microVM + Firecracker + vsock project with limited public training data, tests must be explicit about what "healthy" and "correct" mean.
4. **Hermetic Where Possible** — Expensive components (full daemon + Firecracker) should only be used when necessary. Use contract/fixture modes aggressively.
5. **Security & Isolation Invariants** — Tests must protect the paranoid model (signed Hub communication, ACL boundaries, Court gate, memory isolation, per-VM keys).
6. **Observability in Tests** — Tests should leverage and assert on `boot-metrics`, structured `status`, health endpoints, and clear logs.

## Recommended Test Layers

| Layer                    | Command                        | When to Use                          | LLM Guidance                                                                 | Key Characteristics |
|--------------------------|--------------------------------|--------------------------------------|------------------------------------------------------------------------------|---------------------|
| **Unit**                 | `make test`                    | Every change                         | Fast and isolated. Good for logic that doesn't touch daemon or VMs.          | No privileges needed |
| **Integration**          | `make test-integration`        | Daemon lifecycle, CLI, component registration | Use `-tags=integration`. Focus on startup health and component contracts.    | May require sudo in some cases |
| **Smoke**                | `make smoke`                   | After every `sudo ./bin/aegis start` | Quick sanity that base infrastructure + Court are healthy.                   | Fast, human + LLM friendly |
| **Contract / Fixture**   | `make test-e2e-contract`       | Portal surface, API contracts        | Preferred for CI and rapid iteration. Does not require full daemon.          | Hermetic, fast |
| **Live E2E (Browser)**   | `make test-e2e`                | Full user journeys with real daemon  | Use when testing real microVM behavior, streaming, or Court flows.           | Requires running daemon |
| **Collaboration LLM E2E**| `make test-e2e-llm`            | Project Manager + real LLM + channels| Primary vehicle for validating the collaboration model end-to-end.           | Uses real Ollama path; must assert on startup health first |
| **TCB / Security**       | `make test-tcb`                | Changes touching privileged code     | Focus on key injection, socket security, process isolation.                  | Often requires tags |
| **Chaos / Recovery**     | `make test-chaos` (AEGIS_CHAOS=1) | Failure modes and resilience     | Exercise daemon watchdog, VM death, and recovery paths.                      | Opt-in, powerful for security |

## LLM-Specific Guidance (for Grok Build, Cursor, etc.)

When working on this project as an LLM agent:

- **Always start by understanding the current health model.** Run `make smoke` and inspect `aegis status` + boot metrics before making changes that affect startup or component registration.
- **Prefer contract/fixture mode** (`make test-e2e-contract`) for rapid iteration on portal and API surfaces.
- **For collaboration model work**, use `make test-e2e-llm` as the primary verification. It must pass with real LLM output visible in channels.
- **Explicitly assert startup invariants** in new or modified tests:
  - Base infrastructure (Network Boundary, Store, Web Portal) registers successfully.
  - Court personas (exactly 7) are online.
  - Pre-warm pools exist and are claimable (`aegis vm pools`).
  - No unexpected `aegis-daemon-temp-*` components linger.
  - `aegis status` reports a clean state.
- **Use rich, descriptive assertions** and comments. Future LLM agents will rely on these because training data on Firecracker + custom rootfs + vsock patterns is sparse.
- **Do not skip daemon-dependent tests** due to sudo friction. Follow the instructions in `AGENTS.md` to set up `sudo -n` rules when needed.
- **Update this document and `TESTING.md`** when you introduce new test patterns or change what "healthy" means.
- **Run `make smoke` after any change** that affects daemon startup, pre-warm logic, or component registration.

## Required Test Coverage Areas (Collaboration Model)

- Daemon startup completeness and component registration
- Pre-warm / pooled rootfs behavior and claim performance
- Channel lifecycle (create, membership, post, archive) via both CLI and Portal
- Project Manager goal handling → real LLM plan → dynamic role ensuring → visible in channel
- Auto defaults ("main" channel + Court/PM participation)
- Boot timing and observability (`AEGIS_BOOT_TIMING`)
- Security boundaries (ACL enforcement, signed Hub messages, memory isolation)
- Failure and recovery paths (chaos testing)

## Continuous Improvement

- Treat testing gaps as first-class work items in `docs/implementation-plan/collaboration-model.md`.
- When a bug reaches a human or LLM agent, add or improve an automated test that would have caught it.
- Keep tests fast, deterministic, and honest about their requirements (hermetic vs live).

## Related Documents

- `TESTING.md` — Practical how-to and current implementation details
- `AGENTS.md` — Operational rules (especially daemon start/stop and sudo behavior)
- `docs/implementation-plan/collaboration-model.md` — Current implementation status and open testing tasks
- `Makefile` — All test targets