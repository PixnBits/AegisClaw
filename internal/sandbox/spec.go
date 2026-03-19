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
type NetworkPolicy struct {
	DefaultDeny      bool     `json:"default_deny"`
	AllowedHosts     []string `json:"allowed_hosts,omitempty"`
	AllowedPorts     []uint16 `json:"allowed_ports,omitempty"`
	AllowedProtocols []string `json:"allowed_protocols,omitempty"`
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
	for _, host := range np.AllowedHosts {
		if net.ParseIP(host) == nil {
			if _, _, err := net.ParseCIDR(host); err != nil {
				return fmt.Errorf("invalid allowed host (must be IP or CIDR): %q", host)
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
	return nil
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
