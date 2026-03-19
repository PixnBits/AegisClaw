#!/usr/bin/env python3
"""Writes internal/sandbox/netpolicy.go — network policy engine and nftables parser."""
import os

code = r'''package sandbox

import (
	"fmt"
	"net"
	"strings"
)

// NFTRule represents a single nftables rule to apply.
type NFTRule struct {
	Table    string `json:"table"`
	Chain    string `json:"chain"`
	Rule     string `json:"rule"`
	Family   string `json:"family"`
	Priority int    `json:"priority"`
}

// NFTRuleset contains the full set of nftables rules for a sandbox.
type NFTRuleset struct {
	TableName string    `json:"table_name"`
	ChainName string    `json:"chain_name"`
	Rules     []NFTRule `json:"rules"`
	Teardown  []string  `json:"teardown"`
}

// PolicyEngine converts NetworkPolicy structs into nftables rulesets.
// Default policy: DROP all outbound traffic. Only explicitly allowed
// hosts/ports/protocols are permitted.
type PolicyEngine struct{}

// NewPolicyEngine creates a new policy engine.
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{}
}

// GenerateRuleset converts a NetworkPolicy and sandbox context into nftables rules.
// sandboxID is used to create a unique table name for isolation.
// tapDevice is the host-side tap interface for the sandbox.
func (pe *PolicyEngine) GenerateRuleset(policy *NetworkPolicy, sandboxID string, tapDevice string) (*NFTRuleset, error) {
	if policy == nil {
		return nil, fmt.Errorf("network policy is required")
	}
	if !policy.DefaultDeny {
		return nil, fmt.Errorf("default_deny must be true")
	}
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox ID is required")
	}
	if tapDevice == "" {
		return nil, fmt.Errorf("tap device is required")
	}

	tableName := fmt.Sprintf("aegis_%s", sanitizeID(sandboxID))
	chainName := "output"

	ruleset := &NFTRuleset{
		TableName: tableName,
		ChainName: chainName,
		Rules:     make([]NFTRule, 0),
	}

	// Create table and chain with default DROP
	ruleset.Rules = append(ruleset.Rules, NFTRule{
		Table:  tableName,
		Chain:  chainName,
		Family: "inet",
		Rule:   fmt.Sprintf("add table inet %s", tableName),
	})
	ruleset.Rules = append(ruleset.Rules, NFTRule{
		Table:  tableName,
		Chain:  chainName,
		Family: "inet",
		Rule:   fmt.Sprintf("add chain inet %s %s { type filter hook forward priority 0 ; policy drop ; }", tableName, chainName),
	})

	// Allow established/related connections back
	ruleset.Rules = append(ruleset.Rules, NFTRule{
		Table:  tableName,
		Chain:  chainName,
		Family: "inet",
		Rule:   fmt.Sprintf("add rule inet %s %s iifname %q ct state established,related accept", tableName, chainName, tapDevice),
	})

	// Allow DNS (always permitted for name resolution)
	ruleset.Rules = append(ruleset.Rules, NFTRule{
		Table:  tableName,
		Chain:  chainName,
		Family: "inet",
		Rule:   fmt.Sprintf("add rule inet %s %s iifname %q udp dport 53 accept", tableName, chainName, tapDevice),
	})
	ruleset.Rules = append(ruleset.Rules, NFTRule{
		Table:  tableName,
		Chain:  chainName,
		Family: "inet",
		Rule:   fmt.Sprintf("add rule inet %s %s iifname %q tcp dport 53 accept", tableName, chainName, tapDevice),
	})

	// Generate allow rules from policy
	for _, host := range policy.AllowedHosts {
		hostRules, err := pe.generateHostRules(tableName, chainName, tapDevice, host, policy.AllowedPorts, policy.AllowedProtocols)
		if err != nil {
			return nil, fmt.Errorf("failed to generate rules for host %q: %w", host, err)
		}
		ruleset.Rules = append(ruleset.Rules, hostRules...)
	}

	// If no hosts specified but ports/protocols are, allow those broadly
	if len(policy.AllowedHosts) == 0 && (len(policy.AllowedPorts) > 0 || len(policy.AllowedProtocols) > 0) {
		portRules := pe.generatePortRules(tableName, chainName, tapDevice, policy.AllowedPorts, policy.AllowedProtocols)
		ruleset.Rules = append(ruleset.Rules, portRules...)
	}

	// Log dropped packets for audit
	ruleset.Rules = append(ruleset.Rules, NFTRule{
		Table:  tableName,
		Chain:  chainName,
		Family: "inet",
		Rule:   fmt.Sprintf("add rule inet %s %s iifname %q log prefix \"aegis-drop-%s: \" drop", tableName, chainName, tapDevice, sanitizeID(sandboxID)),
	})

	// Teardown commands to remove the table on sandbox stop
	ruleset.Teardown = []string{
		fmt.Sprintf("delete table inet %s", tableName),
	}

	return ruleset, nil
}

// ToNftCommands converts the ruleset into a list of nft command strings.
func (rs *NFTRuleset) ToNftCommands() []string {
	cmds := make([]string, 0, len(rs.Rules))
	for _, r := range rs.Rules {
		cmds = append(cmds, r.Rule)
	}
	return cmds
}

// TeardownCommands returns the commands to run when destroying the sandbox.
func (rs *NFTRuleset) TeardownCommands() []string {
	return rs.Teardown
}

func (pe *PolicyEngine) generateHostRules(table, chain, tap, host string, ports []uint16, protocols []string) ([]NFTRule, error) {
	var rules []NFTRule

	// Determine if host is an IP or CIDR
	dstMatch := ""
	if net.ParseIP(host) != nil {
		dstMatch = fmt.Sprintf("ip daddr %s", host)
	} else if _, _, err := net.ParseCIDR(host); err == nil {
		dstMatch = fmt.Sprintf("ip daddr %s", host)
	} else {
		return nil, fmt.Errorf("invalid host: %q (must be IP or CIDR)", host)
	}

	if len(protocols) == 0 && len(ports) == 0 {
		// Allow all traffic to this host
		rules = append(rules, NFTRule{
			Table:  table,
			Chain:  chain,
			Family: "inet",
			Rule:   fmt.Sprintf("add rule inet %s %s iifname %q %s accept", table, chain, tap, dstMatch),
		})
		return rules, nil
	}

	protos := protocols
	if len(protos) == 0 {
		protos = []string{"tcp", "udp"}
	}

	for _, proto := range protos {
		if proto == "icmp" {
			rules = append(rules, NFTRule{
				Table:  table,
				Chain:  chain,
				Family: "inet",
				Rule:   fmt.Sprintf("add rule inet %s %s iifname %q %s meta l4proto icmp accept", table, chain, tap, dstMatch),
			})
			continue
		}

		if len(ports) == 0 {
			rules = append(rules, NFTRule{
				Table:  table,
				Chain:  chain,
				Family: "inet",
				Rule:   fmt.Sprintf("add rule inet %s %s iifname %q %s %s dport 1-65535 accept", table, chain, tap, dstMatch, proto),
			})
		} else {
			portList := formatPorts(ports)
			rules = append(rules, NFTRule{
				Table:  table,
				Chain:  chain,
				Family: "inet",
				Rule:   fmt.Sprintf("add rule inet %s %s iifname %q %s %s dport { %s } accept", table, chain, tap, dstMatch, proto, portList),
			})
		}
	}

	return rules, nil
}

func (pe *PolicyEngine) generatePortRules(table, chain, tap string, ports []uint16, protocols []string) []NFTRule {
	var rules []NFTRule

	protos := protocols
	if len(protos) == 0 {
		protos = []string{"tcp", "udp"}
	}

	for _, proto := range protos {
		if proto == "icmp" {
			rules = append(rules, NFTRule{
				Table:  table,
				Chain:  chain,
				Family: "inet",
				Rule:   fmt.Sprintf("add rule inet %s %s iifname %q meta l4proto icmp accept", table, chain, tap),
			})
			continue
		}

		if len(ports) == 0 {
			rules = append(rules, NFTRule{
				Table:  table,
				Chain:  chain,
				Family: "inet",
				Rule:   fmt.Sprintf("add rule inet %s %s iifname %q %s dport 1-65535 accept", table, chain, tap, proto),
			})
		} else {
			portList := formatPorts(ports)
			rules = append(rules, NFTRule{
				Table:  table,
				Chain:  chain,
				Family: "inet",
				Rule:   fmt.Sprintf("add rule inet %s %s iifname %q %s dport { %s } accept", table, chain, tap, proto, portList),
			})
		}
	}

	return rules
}

func formatPorts(ports []uint16) string {
	strs := make([]string, len(ports))
	for i, p := range ports {
		strs[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(strs, ", ")
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteRune(ch)
		} else {
			b.WriteRune('_')
		}
	}
	s := b.String()
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'sandbox', 'netpolicy.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"netpolicy.go: {len(code)} bytes -> {outpath}")
