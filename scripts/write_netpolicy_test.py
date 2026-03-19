#!/usr/bin/env python3
"""Writes internal/sandbox/netpolicy_test.go — tests for the network policy engine."""
import os

code = r'''package sandbox

import (
	"strings"
	"testing"
)

func TestPolicyEngine_DefaultDrop(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(policy, "sandbox-1", "tap0")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	foundDrop := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "policy drop") {
			foundDrop = true
		}
	}
	if !foundDrop {
		t.Fatal("expected default drop policy in chain")
	}
}

func TestPolicyEngine_AllowHost(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:  true,
		AllowedHosts: []string{"10.0.0.1"},
	}

	rs, err := pe.GenerateRuleset(policy, "sb-2", "tap1")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	found := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "ip daddr 10.0.0.1") && strings.Contains(cmd, "accept") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected allow rule for 10.0.0.1")
	}
}

func TestPolicyEngine_AllowCIDR(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:  true,
		AllowedHosts: []string{"192.168.0.0/24"},
	}

	rs, err := pe.GenerateRuleset(policy, "sb-3", "tap2")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	found := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "192.168.0.0/24") && strings.Contains(cmd, "accept") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected allow rule for CIDR")
	}
}

func TestPolicyEngine_AllowPortsAndProtocols(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:      true,
		AllowedHosts:     []string{"10.0.0.5"},
		AllowedPorts:     []uint16{443, 8080},
		AllowedProtocols: []string{"tcp"},
	}

	rs, err := pe.GenerateRuleset(policy, "sb-4", "tap3")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	found443 := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "tcp dport { 443, 8080 }") && strings.Contains(cmd, "10.0.0.5") {
			found443 = true
		}
	}
	if !found443 {
		t.Fatal("expected tcp port 443,8080 rule for 10.0.0.5")
	}
}

func TestPolicyEngine_ICMP(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:      true,
		AllowedHosts:     []string{"10.0.0.1"},
		AllowedProtocols: []string{"icmp"},
	}

	rs, err := pe.GenerateRuleset(policy, "sb-5", "tap4")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	found := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "icmp") && strings.Contains(cmd, "accept") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected icmp allow rule")
	}
}

func TestPolicyEngine_DNSAlwaysAllowed(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(policy, "sb-dns", "tap5")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	foundUDP53 := false
	foundTCP53 := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "udp dport 53") && strings.Contains(cmd, "accept") {
			foundUDP53 = true
		}
		if strings.Contains(cmd, "tcp dport 53") && strings.Contains(cmd, "accept") {
			foundTCP53 = true
		}
	}
	if !foundUDP53 || !foundTCP53 {
		t.Fatal("DNS (port 53) should always be allowed")
	}
}

func TestPolicyEngine_LogAndDrop(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(policy, "sb-log", "tap6")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	lastRule := cmds[len(cmds)-1]
	if !strings.Contains(lastRule, "log prefix") || !strings.Contains(lastRule, "drop") {
		t.Fatalf("expected last rule to log and drop, got: %s", lastRule)
	}
}

func TestPolicyEngine_Teardown(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs, err := pe.GenerateRuleset(policy, "sb-td", "tap7")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	teardown := rs.TeardownCommands()
	if len(teardown) == 0 {
		t.Fatal("expected teardown commands")
	}
	if !strings.Contains(teardown[0], "delete table") {
		t.Fatalf("expected delete table command, got: %s", teardown[0])
	}
}

func TestPolicyEngine_DefaultDenyRequired(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: false}

	_, err := pe.GenerateRuleset(policy, "sb-bad", "tap8")
	if err == nil {
		t.Fatal("expected error when default_deny is false")
	}
}

func TestPolicyEngine_NilPolicy(t *testing.T) {
	pe := NewPolicyEngine()
	_, err := pe.GenerateRuleset(nil, "sb-nil", "tap9")
	if err == nil {
		t.Fatal("expected error for nil policy")
	}
}

func TestPolicyEngine_EmptyID(t *testing.T) {
	pe := NewPolicyEngine()
	_, err := pe.GenerateRuleset(&NetworkPolicy{DefaultDeny: true}, "", "tap10")
	if err == nil {
		t.Fatal("expected error for empty sandbox ID")
	}
}

func TestPolicyEngine_EmptyTap(t *testing.T) {
	pe := NewPolicyEngine()
	_, err := pe.GenerateRuleset(&NetworkPolicy{DefaultDeny: true}, "sb-ok", "")
	if err == nil {
		t.Fatal("expected error for empty tap device")
	}
}

func TestPolicyEngine_InvalidHost(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:  true,
		AllowedHosts: []string{"not-a-valid-host"},
	}

	_, err := pe.GenerateRuleset(policy, "sb-inv", "tap11")
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}

func TestPolicyEngine_PortsWithoutHosts(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{
		DefaultDeny:      true,
		AllowedPorts:     []uint16{80, 443},
		AllowedProtocols: []string{"tcp"},
	}

	rs, err := pe.GenerateRuleset(policy, "sb-ports", "tap12")
	if err != nil {
		t.Fatalf("GenerateRuleset: %v", err)
	}

	cmds := rs.ToNftCommands()
	found := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "tcp dport { 80, 443 }") && strings.Contains(cmd, "accept") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected port-based allow rules when no hosts specified")
	}
}

func TestPolicyEngine_UniqueTableNames(t *testing.T) {
	pe := NewPolicyEngine()
	policy := &NetworkPolicy{DefaultDeny: true}

	rs1, _ := pe.GenerateRuleset(policy, "sandbox-a", "tap0")
	rs2, _ := pe.GenerateRuleset(policy, "sandbox-b", "tap1")

	if rs1.TableName == rs2.TableName {
		t.Fatal("different sandboxes should have different table names")
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"has-dashes", "has_dashes"},
		{"has.dots", "has_dots"},
		{"has/slashes", "has_slashes"},
		{"abc123_OK", "abc123_OK"},
	}

	for _, tc := range tests {
		got := sanitizeID(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestSanitizeID_Truncate(t *testing.T) {
	long := strings.Repeat("a", 50)
	got := sanitizeID(long)
	if len(got) > 32 {
		t.Fatalf("expected max 32 chars, got %d", len(got))
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'sandbox', 'netpolicy_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"netpolicy_test.go: {len(code)} bytes -> {outpath}")
