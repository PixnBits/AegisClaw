# skill_network_policy_test.go — cmd/aegisclaw

## Purpose
Tests that the sandbox network policy is enforced according to skill capabilities declared in the proposal.

## Key Helpers / Tests
- `testProposalStore(t)` — creates a temp proposal store.
- `makeApprovedProposal` — creates a proposal in approved state with specific capability declarations.
- Tests verify: skills with `network` capability get the network-enabled sandbox config; skills without it get network-disabled config. Unapproved proposals cannot activate skills.

## System Fit
Security regression suite for the capability enforcement layer. Uses the real `sandbox` and `proposal` packages with no KVM.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/proposal`
- `github.com/PixnBits/AegisClaw/internal/sandbox`
