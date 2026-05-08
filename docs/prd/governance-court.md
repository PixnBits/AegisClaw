# Governance Court

The Governance Court is the central security and quality gate of AegisClaw. It is a five-persona review board, where each persona runs in its own isolated Firecracker microVM.

## Purpose

Every code change, new skill, prompt modification, or increase in agent autonomy must be approved by the Court before it can be deployed.

## The Five Personas

- **Coder** — Evaluates code quality, maintainability, and correctness
- **Tester** — Focuses on test coverage, edge cases, and validation strategy
- **CISO** — Assesses business risk, compliance, and overall security posture
- **Security Architect** — Performs deep technical security review (attack surface, sandbox escapes, privilege escalation, etc.)
- **User Advocate** — Ensures the change delivers real user value and does not degrade the user experience

## Court Process

1. A formal proposal is submitted
2. All five personas independently review the proposal
3. Each persona votes **Approve**, **Reject**, or **Approve with Conditions**
4. The Court reaches a decision based on configurable voting rules
5. The user receives a clear summary of the Court's findings and has final veto power

## Key Rules

- The Court **must** review every code change, no matter how small
- All Court members run in isolated microVMs — they cannot see each other’s memory or state
- Court discussions and votes are recorded in the tamper-evident audit log
- Enterprise users can configure which personas are required for different types of changes

## Court Scribe

A lightweight Court Scribe component records structured summaries of all user-agent conversations. These summaries are fed to the Court during autonomy promotion reviews and major system changes.

## Related Documents

- **[../architecture.md](../architecture.md)** — High-level system architecture
- **[runtime-architecture.md](./runtime-architecture.md)** — Runtime requirements this drives
- **[../glossary.md](./glossary.md)** — Key term definitions
- **Component Spec** — [../../specs/governance-court.md](../../specs/governance-court.md) (if exists)