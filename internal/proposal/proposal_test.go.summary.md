# proposal_test.go

## Purpose
Unit tests for the `Proposal` FSM, hash computation, network policy validation logic, and the `IsSandboxedLowRisk` auto-approval helper. Tests verify that valid state transitions succeed, invalid transitions return errors, the audit chain hash changes when mutable fields are modified, and the network policy validator correctly rejects dangerous host specifications.

## Key Types and Functions
- `TestProposalFSM`: exercises all valid state transitions and verifies error returned for invalid ones (e.g., directly transitioning from draft to complete)
- `TestComputeHash`: creates two proposals differing in one field and asserts their hashes differ; modifies fields and asserts hash changes
- `TestHashChain`: creates a sequence of proposals linked via PrevHash and verifies chain integrity
- `TestIsSandboxedLowRisk`: tests the auto-approval short-circuit with various risk/capability combinations
- `TestValidateAllowedHost`: exercises `validateAllowedHost` with valid hostnames, IPs, and invalid wildcards/all-zeros CIDRs
- `TestProposalNetworkPolicy`: verifies that proposals with DefaultDeny=false are rejected at creation

## Role in the System
Guards the governance proposal model from regressions in FSM logic and security-critical validation. Since proposals gate all skill deployments, correctness of the FSM and policy validators is non-negotiable.

## Dependencies
- `testing`: standard Go test framework
- `internal/proposal`: package under test
