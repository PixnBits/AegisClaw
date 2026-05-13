// Package sandbox provides abstractions for different sandbox backends.
package sandbox

import (
	"context"
	"crypto/ed25519"
)

// VMConfig contains configuration for starting a sandbox VM.
type VMConfig struct {
	ID              string
	Image           string
	KernelPath      string
	RootfsPath      string
	Memory          uint64 // Memory in MB
	VCpus           int
	PublicKey       ed25519.PublicKey
	NetworkConfig   *NetworkConfig
}

// NetworkConfig specifies network setup for the VM.
type NetworkConfig struct {
	VsockPort uint32
	ExposedPorts []string // e.g., "8080:8080"
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
	Uptime    int64 // seconds
	Memory    uint64 // in MB
	CreatedAt int64 // Unix timestamp
}
