# Governance Court Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Governance Court consists of seven independent microVMs that review every proposed change to the system before it can be merged or deployed. Each persona brings a specialized, critical perspective.

## The Seven Court Personas

- **CISO** — Strategic risk, compliance, and business impact
- **Security Architect** — Technical security design and attack surface
- **Architect** — System design, modularity, and long-term maintainability
- **Senior Coder** — Code quality, readability, and implementation standards
- **Tester** — Testing strategy, coverage, and reliability
- **Efficiency** — Performance, resource usage, and cost implications
- **User Advocate** — Usability, UX, and human impact

## Voting Rules

- All seven personas must respond with a vote
- Valid votes are: `Approve`, `Reject`, or `Abstain`
- A proposal is **Approved** only when it receives **unanimous `Approve`** from all non-abstaining personas
- Any `Reject` vote immediately blocks the proposal
- `Abstain` should be used when a persona lacks sufficient information to judge (common on initial high-level proposals)
- `Needs Work` feedback can be attached to any vote type except `Approve`

## Architecture

- Seven separate Firecracker microVMs (one per persona)
- Each runs its own dedicated LLM with a carefully crafted system prompt
- Court VMs never communicate directly with each other
- All votes must be cryptographically signed

## Output Format Requirements

Each Court VM must return its response in a strict structured format containing:
- `vote`: "Approve" | "Reject" | "Abstain"
- `reasoning`: Detailed explanation from its perspective
- `specific_feedback`: Actionable bullet points (required for `Reject` and `Needs Work`)

## Implementation Guidance

- Each persona must have a highly specialized system prompt that forces it to stay in character
- The same proposal must produce noticeably different feedback from each persona
- Personas should be encouraged to `Abstain` rather than guess when they lack context
- Feedback must be specific enough for the proposer to act on it

## Test Requirements

- Each persona must produce feedback consistent with its specialized role
- All seven votes must be collected before a proposal can advance
- A proposal must not pass without unanimous approval from non-abstaining personas
- Any `Reject` vote must block the proposal
- Court VMs must pull proposal content directly from the Store VM
- A compromised Court VM must not be able to forge or suppress another persona’s vote
- The structured output format must be strictly enforced
