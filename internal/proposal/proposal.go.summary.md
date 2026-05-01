# proposal.go

## Purpose
Defines the core data model and finite state machine (FSM) for governance proposals in the AegisClaw Governance Court SDLC. Every new skill or configuration change must be represented as a `Proposal` that flows through a defined lifecycle of review, approval, and implementation. The file also implements a tamper-evident audit chain via SHA-256 Merkle hashing and enforces network policy validation rules that prevent overly permissive sandbox configurations.

## Key Types and Functions
- `Proposal`: full proposal record (UUID, title, description, category, status, risk, author, target skill, spec, secrets refs, network policy, capabilities, reviews, history, round, MerkleHash, PrevHash, version)
- FSM transitions: draft → submitted → in_review → approved/rejected/escalated → implementing → complete/failed; withdrawn from most states
- `Review`: persona, model, round, verdict (approve/reject/ask/abstain), risk score (0–10), evidence, questions, comments
- `ProposalNetworkPolicy`: DefaultDeny (must be true), AllowedHosts, AllowedPorts, AllowedProtocols, EgressMode (proxy/direct)
- `SkillCapabilities`: Network, FilesystemWrite, HostDevices, Secrets, CanAccessOtherSessions
- `computeHash() string`: SHA-256 over mutable fields forming the audit chain
- `IsSandboxedLowRisk() bool`: auto-approval short-circuit for low-risk sandboxed proposals
- `validateAllowedHost(host) error`: rejects wildcards, `0.0.0.0/0`, and `::/0`

## Role in the System
The `Proposal` is the central document that gates all skill deployments. Every proposal transitions through the Governance Court before any skill code executes, creating an immutable audit trail.

## Dependencies
- `crypto/sha256`, `encoding/json`: hashing and serialisation
- `github.com/google/uuid`: proposal ID generation
- `time`: timestamps for history entries
