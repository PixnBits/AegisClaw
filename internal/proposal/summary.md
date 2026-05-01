# Package: proposal

## Overview
The `proposal` package implements the governance proposal data model and persistence layer for AegisClaw's Governance Court SDLC. Every skill deployment, configuration change, or capability grant must be submitted as a `Proposal` that flows through a defined FSM: draft → submitted → in_review → approved/rejected/escalated → implementing → complete/failed. The model includes a SHA-256 Merkle hash chain for tamper-evidence and a git-backed store that gives every transition a permanent audit record.

## Files
- `proposal.go`: Core `Proposal` data model, FSM transition logic, hash computation, network policy validation, and auto-approval heuristics
- `proposal_test.go`: Unit tests for FSM transitions, hash chain integrity, network policy validation, and `IsSandboxedLowRisk`
- `store.go`: Git-backed store with branch-per-proposal layout and a main-branch JSON index
- `store_test.go`: Integration tests for Create/Update/Get/List/ListByStatus/ResolveID/Import

## Key Abstractions
- `Proposal`: the canonical governance document; carries spec, reviews, network policy, capabilities, and a Merkle hash chain
- `Review`: structured court persona verdict with risk score and evidence
- `ProposalNetworkPolicy`: DefaultDeny-first policy with validated host/port/protocol allowlists
- `SkillCapabilities`: capability flags gating network, filesystem, device, secret, and cross-session access
- FSM: enforces valid state transitions; invalid transitions return descriptive errors
- `computeHash`: SHA-256 over mutable fields; `PrevHash` links reviews into an audit chain

## System Role
The proposal package is the gatekeeper for all skill deployments. The Governance Court TUI (`internal/tui/court_dashboard`), the wizard (`internal/wizard`), and the orchestrator all interact with this package to create, review, and approve proposals before any sandbox is launched.

## Dependencies
- `github.com/go-git/go-git/v5`: branch-per-proposal git storage
- `crypto/sha256`, `encoding/json`: hashing and serialisation
- `github.com/google/uuid`: proposal and review ID generation
