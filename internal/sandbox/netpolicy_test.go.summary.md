# netpolicy_test.go

## Purpose
Tests for the `PolicyEngine` network policy compiler. Verifies that various `NetworkPolicy` configurations produce correct, complete, and correctly ordered nftables rule sets. Tests cover the full range of policy options: no-network mode, allow-list entries, direct vs proxy egress modes, DNS always-allow behaviour, and teardown command generation.

## Key Types and Functions
- `TestGenerateRuleset_NoNetwork`: verifies that a no-network policy produces rules that drop all traffic
- `TestGenerateRuleset_AllowList`: verifies that allowed hosts and ports appear in the generated rule set
- `TestGenerateRuleset_DNSAlwaysAllowed`: verifies DNS rules are always present regardless of the allow list
- `TestGenerateRuleset_DefaultDropAppended`: verifies the log+drop rule is always the last rule in the chain
- `TestGenerateRuleset_DirectMode`: verifies that direct egress mode rejects FQDNs and requires IP/CIDR
- `TestGenerateRuleset_ProxyMode`: verifies that proxy egress mode accepts FQDNs
- `TestTeardownCommands`: verifies that teardown commands reference the correct per-sandbox nftables table name

## Role in the System
Ensures the network isolation enforced on the host matches the policy approved in the governance proposal. Regressions here could allow sandbox network breakout.

## Dependencies
- `testing`
- `internal/sandbox`: `PolicyEngine`, `GenerateRuleset`, `NetworkPolicy`, `SandboxSpec`
