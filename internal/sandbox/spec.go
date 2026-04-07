package sandbox

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"time"
)

// SandboxState represents the lifecycle state of a sandbox.
type SandboxState string

const (
	StateCreated SandboxState = "created"
	StateRunning SandboxState = "running"
	StateStopped SandboxState = "stopped"
	StateError   SandboxState = "error"
)

// Resources defines resource limits for a sandbox microVM.
type Resources struct {
	VCPUs    int64 `json:"vcpus"`
	MemoryMB int64 `json:"memory_mb"`
}

// NetworkPolicy defines network access rules for a sandbox.
// DefaultDeny must always be true; allowed entries selectively open access.
//
// When NoNetwork is true the sandbox receives no TAP device and therefore has
// no IP stack at all — the strongest possible isolation.  AllowedHosts and
// AllowedPorts are ignored when NoNetwork is set.  LLM inference (and any
// other host-service access) must go through the vsock kernel channel instead.
type NetworkPolicy struct {
	// NoNetwork, when true, causes the sandbox to boot with no network interface.
	// Use this for any VM whose only host access is via vsock (e.g. court reviewers
	// that reach Ollama through the host-side LLM proxy).
	NoNetwork        bool     `json:"no_network,omitempty"`
	DefaultDeny      bool     `json:"default_deny"`
	AllowedHosts     []string `json:"allowed_hosts,omitempty"`
	AllowedPorts     []uint16 `json:"allowed_ports,omitempty"`
	AllowedProtocols []string `json:"allowed_protocols,omitempty"`
	// EgressMode controls how outbound traffic is enforced.
	// "proxy" (default) routes traffic through the host-side egress proxy which
	// validates SNI before splicing — strongly preferred for HTTPS/WSS skills.
	// "direct" uses the existing nftables IP/CIDR rules for static destinations.
	EgressMode string `json:"egress_mode,omitempty"`
}

// SandboxSpec defines the desired state of a Firecracker sandbox.
type SandboxSpec struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Resources     Resources     `json:"resources"`
	NetworkPolicy NetworkPolicy `json:"network_policy"`
	SecretsRefs   []string      `json:"secrets_refs,omitempty"`
	VsockCID      uint32        `json:"vsock_cid"`
	RootfsPath    string        `json:"rootfs_path"`
	KernelPath    string        `json:"kernel_path,omitempty"`
	InitPath      string        `json:"init_path,omitempty"`
	WorkspaceMB   int           `json:"workspace_mb"`
}

// SandboxInfo captures the runtime state of a sandbox.
type SandboxInfo struct {
	Spec       SandboxSpec  `json:"spec"`
	State      SandboxState `json:"state"`
	PID        int          `json:"pid,omitempty"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	StoppedAt  *time.Time   `json:"stopped_at,omitempty"`
	Error      string       `json:"error,omitempty"`
	SocketPath string       `json:"socket_path,omitempty"`
	TapDevice  string       `json:"tap_device,omitempty"`
	HostIP     string       `json:"host_ip,omitempty"`
	GuestIP    string       `json:"guest_ip,omitempty"`
}

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)
var secretRefRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]{0,127}$`)

// validHostnameRegex matches a syntactically valid DNS hostname or FQDN.
// Each label: 1-63 chars of letters/digits/hyphens, not starting or ending
// with a hyphen.  Wildcards and empty labels are rejected.
//
// Pattern structure:
//   - (?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*  — zero or more
//     dot-separated labels each 1-63 chars (no leading/trailing hyphen)
//   - [a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$  — final label (same rules)
const hostnamePatternFull = `^(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*` +
	`[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`

var validHostnameRegex = regexp.MustCompile(hostnamePatternFull)

// Validate checks that the SandboxSpec has all required fields with safe values.
func (s *SandboxSpec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("sandbox ID is required")
	}
	if s.Name == "" {
		return fmt.Errorf("sandbox name is required")
	}
	if !nameRegex.MatchString(s.Name) {
		return fmt.Errorf("sandbox name must match pattern %s, got %q", nameRegex.String(), s.Name)
	}
	if s.Resources.VCPUs < 1 || s.Resources.VCPUs > 32 {
		return fmt.Errorf("VCPUs must be between 1 and 32, got %d", s.Resources.VCPUs)
	}
	if s.Resources.MemoryMB < 128 || s.Resources.MemoryMB > 32768 {
		return fmt.Errorf("memory must be between 128 and 32768 MB, got %d", s.Resources.MemoryMB)
	}
	if s.VsockCID < 3 {
		return fmt.Errorf("vsock CID must be >= 3 (0=hypervisor, 1=loopback, 2=host), got %d", s.VsockCID)
	}
	if s.RootfsPath == "" {
		return fmt.Errorf("rootfs path is required")
	}
	if !filepath.IsAbs(s.RootfsPath) {
		return fmt.Errorf("rootfs path must be absolute, got %q", s.RootfsPath)
	}
	if s.KernelPath != "" && !filepath.IsAbs(s.KernelPath) {
		return fmt.Errorf("kernel path must be absolute, got %q", s.KernelPath)
	}
	if !s.NetworkPolicy.DefaultDeny {
		return fmt.Errorf("network policy default_deny must be true")
	}
	if err := validateNetworkPolicy(&s.NetworkPolicy); err != nil {
		return fmt.Errorf("invalid network policy: %w", err)
	}
	for i, ref := range s.SecretsRefs {
		if !secretRefRegex.MatchString(ref) {
			return fmt.Errorf("secrets_refs[%d] %q must match %s", i, ref, secretRefRegex.String())
		}
	}
	return nil
}

func validateNetworkPolicy(np *NetworkPolicy) error {
	proxyMode := np.EgressMode == "" || np.EgressMode == "proxy"
	for _, host := range np.AllowedHosts {
		if proxyMode {
			// In proxy mode, FQDNs (and IPs/CIDRs) are all valid; the egress
			// proxy enforces the list via SNI matching at connect time.
			if !isValidFQDNOrIP(host) {
				return fmt.Errorf("invalid allowed host %q: must be a valid FQDN, IP, or CIDR", host)
			}
		} else {
			// Direct mode: must be IP or CIDR so nftables rules can be applied.
			if net.ParseIP(host) == nil {
				if _, _, err := net.ParseCIDR(host); err != nil {
					return fmt.Errorf("invalid allowed host (must be IP or CIDR in direct mode): %q", host)
				}
			}
		}
	}
	for _, proto := range np.AllowedProtocols {
		switch proto {
		case "tcp", "udp", "icmp":
		default:
			return fmt.Errorf("unsupported protocol %q (allowed: tcp, udp, icmp)", proto)
		}
	}
	switch np.EgressMode {
	case "", "proxy", "direct":
	default:
		return fmt.Errorf("unsupported egress_mode %q (allowed: proxy, direct)", np.EgressMode)
	}
	return nil
}

// isValidFQDNOrIP returns true if s is a valid IPv4/IPv6 address, CIDR, or a
// syntactically valid hostname (FQDN or single label).  It does not perform
// DNS resolution — the check is purely syntactic.
func isValidFQDNOrIP(s string) bool {
	if s == "" {
		return false
	}
	// Accept plain IPs and CIDRs.
	if net.ParseIP(s) != nil {
		return true
	}
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	// Validate as a hostname: labels separated by dots, each label is
	// letters/digits/hyphens, not starting or ending with a hyphen.
	// Wildcards like "*.example.com" are explicitly rejected; use exact FQDNs.
	return validHostnameRegex.MatchString(s)
}

// RuntimeConfig holds paths and settings for the Firecracker runtime.
type RuntimeConfig struct {
	FirecrackerBin string `json:"firecracker_bin"`
	JailerBin      string `json:"jailer_bin"`
	KernelImage    string `json:"kernel_image"`
	RootfsTemplate string `json:"rootfs_template"`
	ChrootBaseDir  string `json:"chroot_base_dir"`
	StateDir       string `json:"state_dir"`
}

// Validate checks that runtime paths are absolute.
func (c *RuntimeConfig) Validate() error {
	checks := map[string]string{
		"firecracker_bin": c.FirecrackerBin,
		"jailer_bin":      c.JailerBin,
		"rootfs_template": c.RootfsTemplate,
		"chroot_base_dir": c.ChrootBaseDir,
		"state_dir":       c.StateDir,
	}
	for name, path := range checks {
		if path == "" {
			return fmt.Errorf("%s is required", name)
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path, got %q", name, path)
		}
	}
	return nil
}
