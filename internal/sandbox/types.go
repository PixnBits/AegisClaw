// Package sandbox provides abstractions for different sandbox backends.
package sandbox

import (
	"context"
	"crypto/ed25519"
)

// VMConfig contains configuration for starting a sandbox VM.
type VMConfig struct {
	ID            string
	Image         string
	KernelPath    string
	RootfsPath    string
	Memory        uint64 // Memory in MB
	VCpus         int
	PublicKey     ed25519.PublicKey
	PrivateKey    ed25519.PrivateKey // per-VM private key for secure injection by backend; daemon must not retain after handoff (TCB)
	NetworkConfig *NetworkConfig
}

// NetworkConfig specifies network setup for the VM.
//
// For Task 7.1 (Network Boundary), we are introducing egress routing concepts.
// The long-term goal is that *no* VM (except the Boundary itself) is allowed
// direct outbound network access. All egress must be explicitly routed through
// the Network Boundary VM (Envoy + control plane) for allowlist enforcement,
// secret injection, and audit.
//
// Fields added for 7.1 integration:
type NetworkConfig struct {
	VsockPort    uint32
	ExposedPorts []string // e.g., "8080:8080"

	// EgressViaBoundary, when true, indicates this VM must have its outbound
	// traffic routed exclusively through the Network Boundary.
	// The sandbox backend is responsible for configuring the VM networking
	// (routes, iptables, vsock proxy, etc.) to enforce this.
	EgressViaBoundary bool

	// BoundaryEgressAddr is the address (host:port or vsock) of the Network
	// Boundary's proxy endpoint that this VM should use for outbound requests.
	// Populated by the orchestrator / Host Daemon when EgressViaBoundary is true.
	BoundaryEgressAddr string

	// BoundarySkillID is the identity this VM should present to the Network
	// Boundary for per-skill allowlist and secret scoping (7.1+).
	BoundarySkillID string
}

// Backend defines the interface for sandbox implementations.
type Backend interface {
	// Start creates and starts a new VM
	Start(ctx context.Context, config VMConfig) error
	// Stop terminates a running VM
	Stop(ctx context.Context, vmID string) error
	// Status returns the current status of a VM
	Status(ctx context.Context, vmID string) (Status, error)
	// List returns all running VMs
	List(ctx context.Context) ([]VMInfo, error)
	// Cleanup performs any necessary cleanup (e.g., on daemon shutdown)
	Cleanup(ctx context.Context) error
}

// Status represents the state of a VM.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
)

// VMInfo contains information about a running VM.
type VMInfo struct {
	ID        string
	Status    Status
	Uptime    int64  // seconds
	Memory    uint64 // in MB
	CreatedAt int64  // Unix timestamp
}
