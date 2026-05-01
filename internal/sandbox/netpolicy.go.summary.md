# netpolicy.go

## Purpose
Implements the `PolicyEngine` which translates a sandbox `NetworkPolicy` into nftables firewall rules. Each sandbox gets its own nftables table (`aegis_<sanitized_id>`) providing complete isolation between sandboxes. The engine always permits DNS (port 53 UDP/TCP), always appends a logging+drop default rule, and generates teardown commands to cleanly remove all rules when a sandbox is destroyed.

## Key Types and Functions
- `PolicyEngine`: holds the sandbox ID and network policy
- `GenerateRuleset(spec SandboxSpec) (*PolicyEngine, error)`: constructs the engine from a spec; validates the policy
- `ToNftCommands() []string`: returns the ordered list of nftables commands to apply the policy
- `TeardownCommands() []string`: returns commands to delete the sandbox's nftables table
- Always allows: DNS (port 53 UDP/TCP)
- Always appends: `log prefix "aegis-drop:" drop` as the final rule
- Direct egress mode: requires IP addresses or CIDR notation in `AllowedHosts`
- Proxy egress mode: accepts FQDNs; DNS resolution is performed by the proxy

## Role in the System
Called by `FirecrackerRuntime.Start` to configure host-side network isolation immediately after a VM starts. The generated nftables rules enforce the network policy from the approved governance proposal, preventing sandbox breakout via network channels.

## Dependencies
- `internal/sandbox`: `SandboxSpec`, `NetworkPolicy` types
- `strings`, `fmt`: rule string construction
- `net`: IP/CIDR parsing for direct mode host validation
