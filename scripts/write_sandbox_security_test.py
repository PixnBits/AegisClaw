#!/usr/bin/env python3
"""Writes internal/sandbox/security_test.go — network enforcement security tests."""
import os

code = r'''package sandbox

import (
	"strings"
	"testing"
)

// TestNetworkPolicy_DefaultDenyBlocksAll verifies that with no allow rules,
// all traffic except DNS is blocked (chain policy is DROP).
func TestNetworkPolicy_DefaultDenyBlocksAll(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(policy, "sec-deny", "tap0")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()

	// Should have: table creation, chain (drop), conntrack, DNS (2), log+drop
	// NO accept rules for arbitrary destinations
	for _, cmd := range cmds {
		if strings.Contains(cmd, "accept") {
			// Only allow: established, DNS, or conntrack
			allowed := strings.Contains(cmd, "ct state") ||
				strings.Contains(cmd, "dport 53")
			if !allowed {
				t.Fatalf("unexpected accept rule in default-deny: %s", cmd)
			}
		}
	}
}

// TestNetworkPolicy_BlockedHostsNotInRules ensures hosts not in AllowedHosts
// have no accept rules generated.
func TestNetworkPolicy_BlockedHostsNotInRules(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:  true,
		AllowedHosts: []string{"10.0.0.1"},
	}

	rs, err := pe.GenerateRuleset(policy, "sec-block", "tap1")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	for _, cmd := range cmds {
		// Should never have 10.0.0.2 or 192.168.x.x
		if strings.Contains(cmd, "10.0.0.2") || strings.Contains(cmd, "192.168") {
			t.Fatalf("rule allows blocked host: %s", cmd)
		}
	}
}

// TestNetworkPolicy_PortRestriction verifies that only specified ports are allowed.
func TestNetworkPolicy_PortRestriction(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:      true,
		AllowedHosts:     []string{"10.0.0.1"},
		AllowedPorts:     []uint16{443},
		AllowedProtocols: []string{"tcp"},
	}

	rs, err := pe.GenerateRuleset(policy, "sec-port", "tap2")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	found443 := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "10.0.0.1") && strings.Contains(cmd, "accept") {
			if strings.Contains(cmd, "443") {
				found443 = true
			}
			// Port 80 should NOT be allowed
			if strings.Contains(cmd, "80") && !strings.Contains(cmd, "443") && !strings.Contains(cmd, "8080") {
				t.Fatalf("port 80 should not be allowed: %s", cmd)
			}
		}
	}
	if !found443 {
		t.Fatal("port 443 should be allowed")
	}
}

// TestNetworkPolicy_AllDropsLogged verifies that dropped packets are logged.
func TestNetworkPolicy_AllDropsLogged(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(policy, "sec-log", "tap3")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	lastCmd := cmds[len(cmds)-1]
	if !strings.Contains(lastCmd, "log") {
		t.Fatal("last rule should log dropped packets")
	}
	if !strings.Contains(lastCmd, "drop") {
		t.Fatal("last rule should drop after logging")
	}
	if !strings.Contains(lastCmd, "aegis-drop") {
		t.Fatal("log prefix should identify the sandbox")
	}
}

// TestNetworkPolicy_IsolatedTables verifies each sandbox has its own table.
func TestNetworkPolicy_IsolatedTables(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs1, _ := pe.GenerateRuleset(policy, "sandbox-alpha", "tap0")
	rs2, _ := pe.GenerateRuleset(policy, "sandbox-beta", "tap1")

	if rs1.TableName == rs2.TableName {
		t.Fatal("sandboxes should have isolated nftables tables")
	}

	// Teardown should only affect own table
	td1 := rs1.TeardownCommands()
	td2 := rs2.TeardownCommands()
	if strings.Contains(td1[0], rs2.TableName) {
		t.Fatal("sandbox-alpha teardown should not reference sandbox-beta table")
	}
	if strings.Contains(td2[0], rs1.TableName) {
		t.Fatal("sandbox-beta teardown should not reference sandbox-alpha table")
	}
}

// TestNetworkPolicy_NoProtocolMeansRestrictive ensures that when no protocols
// are specified, the engine uses safe defaults rather than allowing everything.
func TestNetworkPolicy_NoProtocolMeansRestrictive(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:  true,
		AllowedHosts: []string{"10.0.0.1"},
		AllowedPorts: []uint16{443},
	}

	rs, err := pe.GenerateRuleset(policy, "sec-proto", "tap4")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	// With no protocols specified and ports given, should default to tcp+udp
	foundTCP := false
	foundUDP := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "10.0.0.1") && strings.Contains(cmd, "443") {
			if strings.Contains(cmd, "tcp dport") {
				foundTCP = true
			}
			if strings.Contains(cmd, "udp dport") {
				foundUDP = true
			}
		}
	}
	if !foundTCP {
		t.Fatal("expected tcp rules when no protocols specified")
	}
	if !foundUDP {
		t.Fatal("expected udp rules when no protocols specified")
	}
}

// TestNetworkPolicy_BuilderSandboxFullDeny verifies builder sandboxes
// (without any allowed hosts) have no outbound accept rules.
func TestNetworkPolicy_BuilderSandboxFullDeny(t *testing.T) {
	pe := NewPolicyEngine()
	// Builder sandboxes should have default deny with NO exceptions
	builderPolicy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(builderPolicy, "builder-sandbox-1", "tap-build")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	for _, cmd := range cmds {
		if strings.Contains(cmd, "accept") {
			// Only conntrack and DNS allowed
			if !strings.Contains(cmd, "ct state") && !strings.Contains(cmd, "dport 53") {
				t.Fatalf("builder sandbox should not have accept rules beyond conntrack/DNS: %s", cmd)
			}
		}
	}
}

// TestNetworkPolicy_CourtSandboxFullDeny verifies court sandboxes block all outbound.
func TestNetworkPolicy_CourtSandboxFullDeny(t *testing.T) {
	pe := NewPolicyEngine()
	courtPolicy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(courtPolicy, "court-sandbox-1", "tap-court")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	for _, cmd := range cmds {
		if strings.Contains(cmd, "accept") {
			if !strings.Contains(cmd, "ct state") && !strings.Contains(cmd, "dport 53") {
				t.Fatalf("court sandbox should not have accept rules beyond conntrack/DNS: %s", cmd)
			}
		}
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'sandbox', 'security_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"security_test.go: {len(code)} bytes -> {outpath}")
