# SDLC & Code Development Workflow

**Status:** Draft  
**Last Updated:** June 2026

**Purpose**: Define the end-to-end path for safe, governed self-improvement and skill creation. Every change must be proposed, reviewed by the Governance Court, implemented inside an isolated Builder VM, and merged only through the trusted Store VM.

## Core Flow (One Paragraph for Quick Reference)

User or agent creates a structured Change Proposal → stored and routed via Store VM → Governance Court (via Court Scribe) reviews → on tentative approval a Builder VM is spun up → Builder clones skill repo from Store, creates feature branch, generates and tests code (LLM-driven), commits, pushes, and creates a Pull Request → Court reviews the implementation → on final approval Store performs the merge + skill registration → new capability appears in the registry with full audit trail.

## For Implementers

These documents are the authoritative contracts for anyone (human or automated) extending or implementing the code-development and self-improvement pathways:

- Start here, then read `builder-vm.md` and `store-vm.md` in full.
- Treat the listed responsibilities, "can / cannot", key commands, and security requirements as hard boundaries.
- When generating or modifying code, emit the corresponding `store.*` / `pr.*` / `proposal.*` calls and ensure audit + permission checks are wired.
- See `sdlc-commands.yaml` for a machine-readable summary of the key verbs, owners, and invariants.
- Surface any ambiguity or gap explicitly; propose a minimal update to this or the referenced specs.
- Test coverage expectations: unit (command handling, filtering), integration (Builder ↔ Store round-trips), E2E (full propose → Court → merge → skill visible and invocable).

## Immutable Contracts (Read These First)

- **Builder VM** (`builder-vm.md`): Short-lived, explicitly untrusted microVM acting as a developer workstation. Can clone, branch, generate code, commit, push, create PRs, and respond to Court feedback. **Must not** merge its own PRs, delete branches, or bypass review. All git operations are proxied through the Store VM. Terminates cleanly after use or failure.
- **Store VM** (`store-vm.md`): Sole source of truth for git repositories, PR state, proposals, tamper-evident audit log, and skill registry. Enforces branch protection. Only the Store VM may perform the final merge of an approved PR. Exposes the `git.*`, `pr.*`, `proposal.*`, `skill.register`, and `court.*` command surfaces.
- **Governance Court** (`prd/sdlc-governance.md`, `governance-court.md`): Mandatory review gate for every change (new skills, prompt changes, core components, autonomy levels, Court modifications). Configurable per change type but never optional for high-impact items.
- **Permissions & Visibility** (see `specs/permissions-model.md` once merged): Grants and visibility policies apply to proposal creation, Court actions, Builder operations, and tool discovery. MicroVMs cannot self-grant capabilities.

## Key Invariants (Never Violate)

- No code change reaches the live registry without Court review + final Store merge.
- Builder VMs have no direct host filesystem access and no merge rights.
- Every action is recorded in the append-only Merkle audit log in Store.
- Discovery of tools and skills is always filtered by the subject's current grants + visibility policy.

## Related Documents

- `user-journeys/04-creating-iterating-new-skill.md` — End-to-end user story with testable success criteria.
- `prd/skill-creation.md`, `prd/sdlc-governance.md`, `prd/collaboration-model.md`
- `specs/builder-vm.md`, `specs/store-vm.md`, `specs/sdlc-commands.yaml`, `specs/permissions-model.md` (post-PR 78), `specs/testing-standards.md`
- `docs/prd/index.md` for the broader PRD structure.

## Implementation Notes

This workflow is designed to be self-improving yet structurally safe. When extending (e.g. new CLI verbs, web-portal delegation, additional Builder capabilities, or tighter permission integration), keep the Builder untrusted and the Store authoritative. Update this file, `sdlc-commands.yaml`, and the component specs in lockstep.