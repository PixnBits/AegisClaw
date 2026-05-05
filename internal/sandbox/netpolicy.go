package sandbox

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

// ─── iptables backend (Docker) ────────────────────────────────────────────────

// IPTablesRuleset holds iptables commands for Docker container network policy.
// Rules are keyed on the container source IP so they survive container restarts
// with a new MAC address.
type IPTablesRuleset struct {
	// SandboxID is the AegisClaw sandbox identifier.
	SandboxID string `json:"sandbox_id"`
	// ContainerIP is the container's IP on its per-sandbox bridge network.
	ContainerIP string `json:"container_ip"`
	// ChainName is the per-sandbox iptables chain (e.g. "AEGIS_skill_abc12345").
	ChainName string `json:"chain_name"`
	// Rules are iptables arguments (without "iptables") to apply in order.
	Rules []string `json:"rules"`
	// Teardown are iptables arguments to remove the rules on sandbox stop.
	Teardown []string `json:"teardown"`
}

// ToIPTablesCommands returns Rules as iptables argument slices ready for exec.
func (rs *IPTablesRuleset) ToIPTablesCommands() [][]string {
	cmds := make([][]string, 0, len(rs.Rules))
	for _, r := range rs.Rules {
		cmds = append(cmds, strings.Fields(r))
	}
	return cmds
}

// TeardownIPTablesCommands returns Teardown as iptables argument slices.
func (rs *IPTablesRuleset) TeardownIPTablesCommands() [][]string {
	cmds := make([][]string, 0, len(rs.Teardown))
	for _, r := range rs.Teardown {
		cmds = append(cmds, strings.Fields(r))
	}
	return cmds
}

// GenerateIPTablesRules converts a NetworkPolicy into per-container iptables
// rules.  containerIP must be the IP address assigned to the container on its
// per-sandbox bridge network.
//
// The generated ruleset:
//   - Creates a per-sandbox chain AEGIS_<sanitizedID> in the filter table.
//   - Inserts a jump from the FORWARD chain keyed on the container source IP.
//   - Allows established/related return traffic.
//   - Allows DNS (UDP/TCP port 53) unconditionally.
//   - Allows traffic matching AllowedHosts / AllowedPorts / AllowedProtocols.
//   - Logs and drops all other traffic.
//
// Direct-mode sandboxes (EgressMode="direct") must provide IP/CIDR entries in
// AllowedHosts because iptables rules operate at L3.
func (pe *PolicyEngine) GenerateIPTablesRules(policy *NetworkPolicy, sandboxID, containerIP string) (*IPTablesRuleset, error) {
	if policy == nil {
		return nil, fmt.Errorf("network policy is required")
	}
	if !policy.DefaultDeny {
		return nil, fmt.Errorf("default_deny must be true")
	}
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox ID is required")
	}
	if containerIP == "" {
		return nil, fmt.Errorf("container IP is required")
	}
	if net.ParseIP(containerIP) == nil {
		return nil, fmt.Errorf("container IP %q is not a valid IP address", containerIP)
	}

	chainName := fmt.Sprintf("AEGIS_%s", sanitizeID(sandboxID))

	rs := &IPTablesRuleset{
		SandboxID:   sandboxID,
		ContainerIP: containerIP,
		ChainName:   chainName,
	}

	// 1. Create the per-sandbox chain.
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -N %s", chainName))

	// 2. Jump into the chain from FORWARD for traffic originating from this container.
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -I FORWARD -s %s -j %s", containerIP, chainName))

	// 3. Allow established/related return traffic.
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -A %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", chainName))

	// 4. Allow DNS for hostname resolution.
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -A %s -p udp --dport 53 -j ACCEPT", chainName))
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -A %s -p tcp --dport 53 -j ACCEPT", chainName))

	// 5. Per-host allow rules.
	for _, host := range policy.AllowedHosts {
		hostRules, err := pe.generateIPTablesHostRules(chainName, host, policy.AllowedPorts, policy.AllowedProtocols)
		if err != nil {
			return nil, fmt.Errorf("iptables rules for host %q: %w", host, err)
		}
		rs.Rules = append(rs.Rules, hostRules...)
	}

	// 6. If no hosts but ports/protocols are specified, allow broadly.
	if len(policy.AllowedHosts) == 0 && (len(policy.AllowedPorts) > 0 || len(policy.AllowedProtocols) > 0) {
		rs.Rules = append(rs.Rules, pe.generateIPTablesPortRules(chainName, policy.AllowedPorts, policy.AllowedProtocols)...)
	}

	// 7. Log and drop everything else.
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -A %s -j LOG --log-prefix \"aegis-drop-%s: \"", chainName, sanitizeID(sandboxID)))
	rs.Rules = append(rs.Rules, fmt.Sprintf("-t filter -A %s -j DROP", chainName))

	// Teardown: remove FORWARD jump, flush chain, delete chain.
	rs.Teardown = []string{
		fmt.Sprintf("-t filter -D FORWARD -s %s -j %s", containerIP, chainName),
		fmt.Sprintf("-t filter -F %s", chainName),
		fmt.Sprintf("-t filter -X %s", chainName),
	}

	return rs, nil
}

func (pe *PolicyEngine) generateIPTablesHostRules(chain, host string, ports []uint16, protocols []string) ([]string, error) {
	var rules []string
	dstMatch := ""
	if net.ParseIP(host) != nil {
		dstMatch = fmt.Sprintf("-d %s", host)
	} else if _, _, err := net.ParseCIDR(host); err == nil {
		dstMatch = fmt.Sprintf("-d %s", host)
	} else {
		return nil, fmt.Errorf("invalid host %q: must be an IP or CIDR in direct mode", host)
	}

	if len(protocols) == 0 && len(ports) == 0 {
		rules = append(rules, fmt.Sprintf("-t filter -A %s %s -j ACCEPT", chain, dstMatch))
		return rules, nil
	}

	protos := protocols
	if len(protos) == 0 {
		protos = []string{"tcp", "udp"}
	}
	for _, proto := range protos {
		if proto == "icmp" {
			rules = append(rules, fmt.Sprintf("-t filter -A %s %s -p icmp -j ACCEPT", chain, dstMatch))
			continue
		}
		if len(ports) == 0 {
			rules = append(rules, fmt.Sprintf("-t filter -A %s %s -p %s -j ACCEPT", chain, dstMatch, proto))
		} else {
			rules = append(rules, fmt.Sprintf("-t filter -A %s %s -p %s -m multiport --dports %s -j ACCEPT", chain, dstMatch, proto, formatPortsComma(ports)))
		}
	}
	return rules, nil
}

func (pe *PolicyEngine) generateIPTablesPortRules(chain string, ports []uint16, protocols []string) []string {
	var rules []string
	protos := protocols
	if len(protos) == 0 {
		protos = []string{"tcp", "udp"}
	}
	for _, proto := range protos {
		if proto == "icmp" {
			rules = append(rules, fmt.Sprintf("-t filter -A %s -p icmp -j ACCEPT", chain))
			continue
		}
		if len(ports) == 0 {
			rules = append(rules, fmt.Sprintf("-t filter -A %s -p %s -j ACCEPT", chain, proto))
		} else {
			rules = append(rules, fmt.Sprintf("-t filter -A %s -p %s -m multiport --dports %s -j ACCEPT", chain, proto, formatPortsComma(ports)))
		}
	}
	return rules
}

// formatPortsComma formats a port list as comma-separated values for iptables
// --multiport --dports (e.g. "80,443,8080").
func formatPortsComma(ports []uint16) string {
	strs := make([]string, len(ports))
	for i, p := range ports {
		strs[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(strs, ",")
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
